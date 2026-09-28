package transcodercaps

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

// transcoder is a fake content-transcoder whose answer the test changes.
type transcoder struct {
	mu     sync.Mutex
	status int
	body   string
	delay  time.Duration
	asked  atomic.Int64
}

func (f *transcoder) set(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

func (f *transcoder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.asked.Add(1)
	f.mu.Lock()
	status, body, delay := f.status, f.body, f.delay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if r.URL.Path != "/capabilities" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

const (
	answerOn  = `{"passthrough_video_codecs":["hevc"]}` + "\n"
	answerOff = `{"passthrough_video_codecs":[]}` + "\n"
)

func fakeService(t *testing.T, f *transcoder) (*Service, *time.Time) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := newService(srv.URL+"/capabilities", &http.Client{Timeout: 3 * time.Second}, time.Hour, func() time.Time { return now })
	return s, &now
}

// Anything but a 200 with the key is no answer: cold, the service stays
// Unknown -- never Off. A transcoder without the endpoint (404), one that
// fails, one that says something else, one that says nothing.
func TestNotAnAnswerIsNotOff(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
	}{
		"404 (a transcoder without the endpoint)": {404, "404 page not found\n"},
		"500":                  {500, "boom"},
		"503":                  {503, ""},
		"garbage":              {200, "<html>proxy error</html>"},
		"json without the key": {200, `{"codecs":["hevc"]}`},
		"null":                 {200, `{"passthrough_video_codecs":null}`},
	} {
		t.Run(name, func(t *testing.T) {
			f := &transcoder{status: c.status, body: c.body}
			s, _ := fakeService(t, f)
			s.Poll()
			if got := s.HEVCPassthrough(); got != Unknown {
				t.Errorf("got %q, want unknown", got)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		f := &transcoder{status: 200, body: answerOn, delay: requestTimeout + 500*time.Millisecond}
		s, _ := fakeService(t, f)
		s.Poll()
		if got := s.HEVCPassthrough(); got != Unknown {
			t.Errorf("got %q, want unknown", got)
		}
	})
	t.Run("the empty list is an answer: off", func(t *testing.T) {
		s, _ := fakeService(t, &transcoder{status: 200, body: answerOff})
		s.Poll()
		if got := s.HEVCPassthrough(); got != Off {
			t.Errorf("got %q, want off", got)
		}
	})
}

// The answer is about configuration, not liveness: once heard it stays
// until the transcoder says something else, however long it then fails to
// answer. Unknown is only the time before the first answer.
func TestLastAnswerOutlivesFailures(t *testing.T) {
	f := &transcoder{status: 404, body: ""}
	s, now := fakeService(t, f)
	s.Poll()
	if got := s.HEVCPassthrough(); got != Unknown {
		t.Fatalf("cold, failing: %q", got)
	}
	f.set(200, answerOn)
	s.Poll()
	if got := s.HEVCPassthrough(); got != On {
		t.Fatalf("answered on: %q", got)
	}
	// A day of failures -- a rollout, a partition -- polled every 30 s.
	f.set(503, "")
	for i := 0; i < 2880; i++ {
		*now = now.Add(PollInterval)
		s.Poll()
		if got := s.HEVCPassthrough(); got != On {
			t.Fatalf("after %v without an answer: %q, want on", time.Duration(i+1)*PollInterval, got)
		}
	}
	// A new answer replaces it at once.
	f.set(200, answerOff)
	s.Poll()
	if got := s.HEVCPassthrough(); got != Off {
		t.Fatalf("answered off: %q", got)
	}
}

// A read is a field read: it neither waits for a question in flight nor
// starts one.
func TestReadNeverWaitsForTheTranscoder(t *testing.T) {
	f := &transcoder{status: 200, body: answerOn, delay: 1500 * time.Millisecond}
	s, _ := fakeService(t, f)
	done := make(chan struct{})
	go func() { s.Poll(); close(done) }()
	// Let the poll reach the server.
	for f.asked.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	got := s.HEVCPassthrough()
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("a read took %v while a question was in flight", d)
	}
	if got != Unknown {
		t.Errorf("before the first answer: %q", got)
	}
	<-done
	if got := s.HEVCPassthrough(); got != On {
		t.Errorf("after it: %q", got)
	}
}

// Pages do not cause questions: a hundred concurrent reads between two polls
// ask nothing.
func TestReadsAskNothing(t *testing.T) {
	f := &transcoder{status: 200, body: answerOn}
	s, _ := fakeService(t, f)
	s.Poll()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.HEVCPassthrough() }()
	}
	wg.Wait()
	if n := f.asked.Load(); n != 1 {
		t.Errorf("%d questions, want the 1 poll", n)
	}
}

// The background loop asks at once and then every interval, and stops on
// Close.
func TestStartPollsUntilClosed(t *testing.T) {
	f := &transcoder{status: 200, body: answerOn}
	srv := httptest.NewServer(f)
	defer srv.Close()
	s := newService(srv.URL+"/capabilities", srv.Client(), 20*time.Millisecond, time.Now)
	s.Start()
	deadline := time.Now().Add(2 * time.Second)
	for s.HEVCPassthrough() != On && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.HEVCPassthrough() != On {
		t.Fatal("never answered")
	}
	for f.asked.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.asked.Load() < 3 {
		t.Fatalf("asked %d times in 2 s at a 20 ms interval", f.asked.Load())
	}
	s.Close()
	time.Sleep(50 * time.Millisecond)
	after := f.asked.Load()
	time.Sleep(100 * time.Millisecond)
	if f.asked.Load() != after {
		t.Error("still asking after Close")
	}
}

// Without an address the answer is Unknown, nothing is asked, and the log
// says why -- once.
func TestNoAddressIsUnknownAndSaidOnce(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	s := newService("", http.DefaultClient, time.Millisecond, time.Now)
	s.Start()
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 10; i++ {
		if got := s.HEVCPassthrough(); got != Unknown {
			t.Fatalf("got %q", got)
		}
	}
	n := 0
	for _, e := range hook.AllEntries() {
		if strings.Contains(e.Message, "no content-transcoder address") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d lines about the missing address, want 1", n)
	}
	var nilService *Service
	if nilService.HEVCPassthrough() != Unknown {
		t.Error("nil service: not unknown")
	}
}

// The log says every change of answer and every new reason for not getting
// one -- not every failed poll.
func TestLogsChangesNotPolls(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	log.SetLevel(log.InfoLevel)
	f := &transcoder{status: 200, body: answerOn}
	s, _ := fakeService(t, f)
	s.Poll()
	s.Poll()
	f.set(503, "")
	for i := 0; i < 5; i++ {
		s.Poll()
	}
	f.set(200, answerOn)
	s.Poll()
	f.set(200, answerOff)
	s.Poll()
	var lines []string
	for _, e := range hook.AllEntries() {
		if strings.HasPrefix(e.Message, "transcoder capability:") {
			lines = append(lines, e.Message)
		}
	}
	want := []string{
		"transcoder capability: on",
		"transcoder capability: no answer, keeping the last one",
		"transcoder capability: answering again",
		"transcoder capability: off",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("log lines\n %q\nwant\n %q", lines, want)
	}
}
