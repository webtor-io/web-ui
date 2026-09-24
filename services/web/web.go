package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	webHostFlag         = "host"
	webPortFlag         = "port"
	shutdownTimeoutFlag = "shutdown-timeout"
	StagingFlag         = "staging"
	RedirectDomainFlag  = "redirect-domain"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   webHostFlag,
			Usage:  "listening host",
			Value:  "",
			EnvVar: "WEB_HOST",
		},
		cli.IntFlag{
			Name:   webPortFlag,
			Usage:  "http listening port",
			Value:  8080,
			EnvVar: "WEB_PORT",
		},
		cli.DurationFlag{
			Name:   shutdownTimeoutFlag,
			Usage:  "how long to let in-flight requests finish on SIGTERM; keep below terminationGracePeriodSeconds minus the preStop sleep",
			Value:  20 * time.Second,
			EnvVar: "WEB_SHUTDOWN_TIMEOUT",
		},
		cli.BoolFlag{
			Name:   StagingFlag,
			Usage:  "mark deployment as staging: forces X-Robots-Tag noindex on every response",
			EnvVar: "STAGING",
		},
		cli.StringFlag{
			Name:   RedirectDomainFlag,
			Usage:  "redirect requests for hosts other than the canonical domain to this URL (e.g. https://webtor.io)",
			EnvVar: "REDIRECT_DOMAIN",
		},
	)
}

type Web struct {
	host            string
	port            int
	shutdownTimeout time.Duration
	mu              sync.Mutex
	srv             *http.Server
	r               *gin.Engine
	handler         http.Handler
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to web listen to tcp connection")
	}
	log.Infof("serving web at %v", addr)
	h := s.handler
	if h == nil {
		h = s.r
	}
	srv := &http.Server{Handler: h}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close drains the server: it stops accepting, closes idle keep-alive
// connections and waits for in-flight requests up to the shutdown timeout,
// then cuts whatever is still open (long-lived streams). Closing the
// listener alone let the process exit mid-response, and the ingress answered
// 502 on every request a terminating pod was still serving.
//
// It must run before the dependencies the handlers use are closed, so
// serve() calls it explicitly rather than leaving it to defer order.
func (s *Web) Close() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.mu.Unlock()
	if srv == nil {
		return
	}
	log.WithField("timeout", s.shutdownTimeout).Info("closing web")
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.WithError(err).Warn("web shutdown timed out, closing remaining connections")
		_ = srv.Close()
	}
	log.Info("web closed")
}

// Use wraps the Gin engine with an HTTP-level middleware.
// Middleware added via Use runs BEFORE Gin's router, which is needed
// for things like language-prefix stripping that must rewrite the URL
// path before route matching.
func (s *Web) Use(mw func(http.Handler) http.Handler) {
	if s.handler == nil {
		s.handler = s.r
	}
	s.handler = mw(s.handler)
}

func New(c *cli.Context, r *gin.Engine) (*Web, error) {
	r.UseRawPath = true

	return &Web{
		host:            c.String(webHostFlag),
		port:            c.Int(webPortFlag),
		shutdownTimeout: c.Duration(shutdownTimeoutFlag),
		r:               r,
	}, nil
}
