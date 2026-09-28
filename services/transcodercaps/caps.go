// Package transcodercaps answers one question for the pages that must not
// promise what the transcoder will not do: does content-transcoder hand HEVC
// to the player as it is (HEVC passthrough)?
//
// The answer is content-transcoder's own, from GET /capabilities -- the
// capability exactly as a POST /session opened now reads it, not a copy of
// its configuration (a build that cannot write a codec, or a capability file
// it cannot read, would make a copy lie).
//
// Three answers, and the third is not the second:
//
//   - On / Off: the transcoder said so. The last thing it said stays the
//     answer until it says something else. The question is "is it
//     configured", not "does it answer right now": a transcoder that is
//     restarting, or a network blip, does not turn a configured capability
//     into an absent one (web-ui CLAUDE.md, gating by capability).
//   - Unknown: this process has not heard an answer yet -- the first seconds
//     after start, no address configured, or a transcoder that has never
//     answered the question (one without the endpoint answers 404). Callers
//     must not read it as Off.
//
// The transcoder is asked in the background, every PollInterval, whether or
// not a page asks: a read never waits for it and never triggers it, so the
// answer a page gets is never older than one interval plus a timeout, and
// never "unknown" because nobody happened to look for a while.
//
// The playback path does not read this at all: a stream's route is decided
// per session by the transcoder from the browser's declaration.
package transcodercaps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/metrics"
)

const (
	hostFlag = "content-transcoder-host"
	portFlag = "content-transcoder-port"
)

// PollInterval is how often the transcoder is asked. A switch of the
// capability reaches Discover within this (plus the transcoder's own
// ConfigMap delay); the cost is one small GET per pod per interval.
const PollInterval = 30 * time.Second

// requestTimeout bounds one question; the client's own timeout is a
// backstop above it.
const requestTimeout = 2 * time.Second

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   hostFlag,
			Usage:  "content-transcoder host, asked GET /capabilities for Discover (auto-injected by kubernetes; self-hosted sets it); empty: HEVC passthrough is unknown",
			EnvVar: "CONTENT_TRANSCODER_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   portFlag,
			Usage:  "content-transcoder port",
			EnvVar: "CONTENT_TRANSCODER_SERVICE_PORT",
			Value:  80,
		},
	)
}

// Answer is the transcoder's answer to "is HEVC passed through".
type Answer string

const (
	On      Answer = "on"
	Off     Answer = "off"
	Unknown Answer = "unknown"
)

type Service struct {
	url      string
	cl       *http.Client
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	answer  Answer
	since   time.Time // when answer took its value
	failing string    // why the last poll got no answer ("" after one that did)

	stop chan struct{}
	once sync.Once
}

// New reads the address from the flags. Without one the service answers
// Unknown and never asks anything; the log says so once, at start.
func New(c *cli.Context) *Service {
	host := c.String(hostFlag)
	u := ""
	if host != "" {
		u = fmt.Sprintf("http://%s/capabilities", net.JoinHostPort(host, fmt.Sprint(c.Int(portFlag))))
	}
	return newService(u, &http.Client{Timeout: requestTimeout + time.Second}, PollInterval, time.Now)
}

func newService(url string, cl *http.Client, interval time.Duration, now func() time.Time) *Service {
	return &Service{
		url:      url,
		cl:       cl,
		interval: interval,
		now:      now,
		answer:   Unknown,
		since:    now(),
		stop:     make(chan struct{}),
	}
}

// Start begins asking in the background. Not a cs.Servable: a servable that
// returns ends the process, and a deployment without an address has nothing
// to serve (the same shape as the offer catalog).
func (s *Service) Start() {
	if s == nil {
		return
	}
	metrics.TranscoderCapability(string(Unknown))
	if s.url == "" {
		log.Info("transcoder capability: no content-transcoder address (CONTENT_TRANSCODER_SERVICE_HOST); Discover treats HEVC passthrough as unknown")
		return
	}
	log.WithField("url", s.url).Info("transcoder capability: reading GET /capabilities in the background")
	go s.loop()
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
}

func (s *Service) loop() {
	for {
		s.Poll()
		select {
		case <-s.stop:
			return
		case <-time.After(s.interval):
		}
	}
}

// HEVCPassthrough is the answer as last heard. It never blocks and never
// asks the transcoder. A nil service (no service configured at all) answers
// Unknown.
func (s *Service) HEVCPassthrough() Answer {
	if s == nil {
		return Unknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answer
}

// Poll asks the transcoder once. A question that gets no answer changes
// nothing: the last answer stays, and Unknown stays Unknown.
func (s *Service) Poll() {
	codecs, why := s.ask()
	s.mu.Lock()
	defer s.mu.Unlock()
	if why != "" {
		metrics.TranscoderCapabilityCheck("failed")
		if why != s.failing {
			log.WithFields(log.Fields{
				"reason": why,
				"kept":   string(s.answer),
				"since":  s.since.UTC().Format(time.RFC3339),
			}).Warn("transcoder capability: no answer, keeping the last one")
		}
		s.failing = why
		return
	}
	a := Off
	for _, c := range codecs {
		if c == "hevc" {
			a = On
		}
	}
	metrics.TranscoderCapabilityCheck(string(a))
	recovered := s.failing != ""
	s.failing = ""
	if a == s.answer {
		if recovered {
			log.WithField("answer", string(a)).Info("transcoder capability: answering again")
		}
		return
	}
	now := s.now()
	log.WithFields(log.Fields{
		"reason": "answered",
		"was":    string(s.answer),
		"since":  s.since.UTC().Format(time.RFC3339),
	}).Info("transcoder capability: " + string(a))
	s.answer, s.since = a, now
	metrics.TranscoderCapability(string(a))
}

// capabilitiesResponse is content-transcoder's answer. A pointer, so that a
// body without the key -- or with null -- is told apart from an empty list:
// only the key itself is an answer.
type capabilitiesResponse struct {
	PassthroughVideoCodecs *[]string `json:"passthrough_video_codecs"`
}

// ask returns the codecs passed through, or why there is no answer.
func (s *Service) ask() ([]string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, "bad address"
	}
	res, err := s.cl.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return nil, "timeout"
		}
		return nil, "unreachable"
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("http %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		return nil, "unreadable"
	}
	var cr capabilitiesResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, "not json"
	}
	if cr.PassthroughVideoCodecs == nil {
		return nil, "no passthrough_video_codecs"
	}
	return *cr.PassthroughVideoCodecs, ""
}
