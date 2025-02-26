package job

import (
	"context"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type Observer struct {
	C      chan LogItem
	ID     string
	mux    sync.Mutex
	closed bool
}

func (s *Observer) Push(ctx context.Context, v LogItem) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.closed {
		return
	}
	select {
	case <-ctx.Done():
		return
	case s.C <- v:
		return
	}
}

func (s *Observer) Close() {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.C)
}

func NewObserver() *Observer {
	return &Observer{
		C:  make(chan LogItem),
		ID: uuid.New().String(),
	}
}

type Job struct {
	ID        string
	Queue     string
	l         []LogItem
	runnable  Runnable
	observers map[string]*Observer
	closed    bool
	mux       sync.Mutex
	cur       string
	Context   context.Context
	storage   Storage
	main      bool
	purge     bool
}

type LogItemLevel string

const (
	Info           LogItemLevel = "info"
	Error          LogItemLevel = "error"
	Warn           LogItemLevel = "warn"
	Done           LogItemLevel = "done"
	InProgress     LogItemLevel = "inprogress"
	Redirect       LogItemLevel = "redirect"
	Download       LogItemLevel = "download"
	RenderTemplate LogItemLevel = "rendertemplate"
	Custom         LogItemLevel = "custom"
	StatusUpdate   LogItemLevel = "statusupdate"
	Close          LogItemLevel = "close"
	Open           LogItemLevel = "open"
)

var levelMap = map[LogItemLevel]log.Level{
	Info:           log.InfoLevel,
	Open:           log.InfoLevel,
	Error:          log.ErrorLevel,
	Warn:           log.WarnLevel,
	Done:           log.InfoLevel,
	InProgress:     log.InfoLevel,
	Download:       log.InfoLevel,
	Redirect:       log.InfoLevel,
	StatusUpdate:   log.InfoLevel,
	RenderTemplate: log.InfoLevel,
	Custom:         log.InfoLevel,
	Close:          log.InfoLevel,
}

type LogItem struct {
	Level     LogItemLevel `json:"level,omitempty"`
	Message   string       `json:"message,omitempty"`
	Status    string       `json:"status,omitempty"`
	Tag       string       `json:"tag,omitempty"`
	Location  string       `json:"location,omitempty"`
	Template  string       `json:"template,omitempty"`
	Body      string       `json:"body,omitempty"`
	Timestamp time.Time    `json:"timestamp,omitempty"`
}

func New(ctx context.Context, id string, queue string, runnable Runnable, storage Storage, purge bool) *Job {
	return &Job{
		ID:        id,
		Queue:     queue,
		runnable:  runnable,
		Context:   ctx,
		l:         []LogItem{},
		observers: map[string]*Observer{},
		storage:   storage,
		main:      true,
		purge:     purge,
	}
}

func (s *Job) Run(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("job panic: %v", r)
		}
	}()
	defer s.close()
	s.open()
	if !s.purge {
		items, err := s.storage.Sub(ctx, s.ID)
		if err != nil {
			return err
		}
		if items != nil {
			s.main = false
			for i := range items {
				if i.Level == Close {
					s.close()
				}
				err = s.log(i)
				if err != nil {
					return err
				}
			}
			return nil
		}
	} else {
		err := s.storage.Drop(ctx, s.ID)
		if err != nil {
			return err
		}
	}

	if s.runnable != nil {
		err := s.runnable.Run(ctx, s)
		if err != nil {
			errr := s.Error(err)
			if errr != nil {
				return errr
			}
			return err
		}
	}
	return nil
}

func (s *Job) ObserveLog() *Observer {
	o := NewObserver()
	s.observers[o.ID] = o
	return o
}

func (s *Job) pushToObservers(ctx context.Context, l LogItem) {
	wg := sync.WaitGroup{}
	wg.Add(len(s.observers))
	for _, o := range s.observers {
		go func(o *Observer) {
			o.Push(ctx, l)
			wg.Done()
		}(o)
	}
	wg.Wait()
}

func (s *Job) pubToStorage(l LogItem) (err error) {
	if l.Level == Open {
		return
	}
	return s.storage.Pub(s.Context, s.ID, l)
}

func (s *Job) logToLogger(l LogItem) {
	message := l.Message
	if message == "" {
		message = string(l.Level)
	}
	log.WithFields(log.Fields{
		"ID":       s.ID,
		"Queue":    s.Queue,
		"Tag":      l.Tag,
		"Location": l.Location,
		"Template": l.Template,
		"Body":     l.Body,
		"Status":   l.Status,
	}).Log(levelMap[l.Level], message)
}

func (s *Job) log(l LogItem) error {
	l.Timestamp = time.Now()
	if l.Level == InProgress {
		s.cur = l.Tag
	} else if l.Tag == "" {
		l.Tag = s.cur
	}
	s.l = append(s.l, l)

	if s.main {
		err := s.pubToStorage(l)
		if err != nil {
			return err
		}
	}

	s.logToLogger(l)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.pushToObservers(ctx, l)

	return nil
}

func (s *Job) open() *Job {
	_ = s.log(LogItem{
		Level: Open,
	})
	return s
}

func (s *Job) Info(message string) *Job {
	_ = s.log(LogItem{
		Level:   Info,
		Message: message,
	})
	return s
}

func (s *Job) Warn(err error) *Job {
	log.WithError(err).Warn("got job warning")
	_ = s.log(LogItem{
		Level:   Warn,
		Message: makeErrorMessage(err),
		Tag:     s.cur,
	})
	return s
}

func makeErrorMessage(err error) string {
	msg := strings.Split(err.Error(), ":")[0]
	if errors.Is(err, context.DeadlineExceeded) {
		return msg + " (deadline exceeded)"
	}
	return msg
}

func (s *Job) Error(err error) error {
	log.WithError(err).Error("got job error")
	_ = s.log(LogItem{
		Level:   Error,
		Message: makeErrorMessage(err),
		Tag:     s.cur,
	})
	return err
}

func (s *Job) InProgress(message string) *Job {
	s.cur = message
	_ = s.log(LogItem{
		Level:   InProgress,
		Message: message,
		Tag:     s.cur,
	})
	return s
}

func (s *Job) StatusUpdate(status string) *Job {
	_ = s.log(LogItem{
		Level:  StatusUpdate,
		Status: status,
		Tag:    s.cur,
	})
	return s
}

func (s *Job) Done() *Job {
	_ = s.log(LogItem{
		Level: Done,
		Tag:   s.cur,
	})
	return s
}

func (s *Job) DoneWithMessage(msg string) *Job {
	_ = s.log(LogItem{
		Level:   Done,
		Tag:     s.cur,
		Message: msg,
	})
	return s
}

func (s *Job) Download(url string) *Job {
	_ = s.log(LogItem{
		Level:    Download,
		Message:  "success! download should start right now!",
		Location: url,
	})
	return s
}

func (s *Job) Redirect(url string) *Job {
	_ = s.log(LogItem{
		Level:    Redirect,
		Message:  "success! redirecting",
		Location: url,
	})
	return s
}

func (s *Job) RenderTemplate(tag string, name string, body string) *Job {
	_ = s.log(LogItem{
		Level:    RenderTemplate,
		Template: name,
		Tag:      tag,
		Body:     body,
	})
	return s
}

func (s *Job) close() {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.log(LogItem{
		Level: Close,
	})
	for _, o := range s.observers {
		o.Close()
	}
}

func (s *Job) Custom(name string, body string) *Job {
	_ = s.log(LogItem{
		Level:    Custom,
		Template: name,
		Body:     body,
	})
	return s
}

type Jobs struct {
	queue   string
	mux     sync.Mutex
	jobs    map[string]*Job
	storage Storage
}

func newJobs(queue string, storage Storage) *Jobs {
	return &Jobs{
		queue:   queue,
		jobs:    map[string]*Job{},
		storage: storage,
	}
}

func (s *Jobs) Enqueue(ctx context.Context, cancel context.CancelFunc, id string, r Runnable, purge bool) *Job {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, ok := s.jobs[id]; ok && !purge {
		return s.jobs[id]
	}
	j := New(ctx, id, s.queue, r, s.storage, purge)
	s.jobs[id] = j
	go func() {
		defer cancel()
		err := j.Run(ctx)
		<-time.After(60 * time.Second)
		if err != nil {
			_ = s.storage.Drop(context.Background(), id)
			log.WithError(err).Error("got job error")
		}
		s.mux.Lock()
		defer s.mux.Unlock()
		delete(s.jobs, id)
	}()
	return j
}

func (s *Jobs) Log(ctx context.Context, id string) (c chan LogItem, ok bool, err error) {
	c = make(chan LogItem, 10)
	j, ok := s.jobs[id]
	if !ok {
		log.Infof("unable to find local job with id=%v", id)
		var state *State
		state, ok, err = s.storage.GetState(ctx, id)
		log.Infof("got storage state=%+v for id=%+v err=%+v", state, id, err)
		if err != nil {
			close(c)
			return
		}
		if !ok {
			log.Warnf("no state for id=%+v", id)
			close(c)
			return
		}
		jCtx, cancel := context.WithTimeout(ctx, state.TTL)
		j = s.Enqueue(jCtx, cancel, id, nil, false)
	} else {
		log.Infof("found local job with id=%+v", id)
	}
	go func() {
		for _, i := range j.l {
			c <- i
		}
		if j.closed {
			close(c)
		} else {
			o := j.ObserveLog()
			for {
				select {
				case <-ctx.Done():
					close(c)
					return
				case i, okk := <-o.C:
					if !okk {
						close(c)
						return
					}
					c <- i
					if i.Level == Close {
						close(c)
						return
					}
				}
			}
		}
	}()
	return
}

type Queues struct {
	jobs    map[string]*Jobs
	storage Storage
}

var queueMux sync.Mutex

func NewQueues(storage Storage) *Queues {
	return &Queues{
		jobs:    map[string]*Jobs{},
		storage: storage,
	}
}

func (s Queues) GetOrCreate(name string) *Jobs {
	queueMux.Lock()
	defer queueMux.Unlock()
	_, ok := s.jobs[name]
	if !ok {
		s.jobs[name] = newJobs(name, s.storage)
	}
	return s.jobs[name]
}

type Runnable interface {
	Run(ctx context.Context, j *Job) error
}

type Script struct {
	body func(j *Job) error
}

func (s *Script) Run(ctx context.Context, j *Job) error {
	return s.body(j)
}

func NewScript(body func(j *Job) error) *Script {
	return &Script{body: body}
}
