package web

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	log "github.com/sirupsen/logrus"
)

const (
	webHostFlag        = "host"
	webPortFlag        = "port"
	StagingFlag        = "staging"
	RedirectDomainFlag = "redirect-domain"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	f = cs.RegisterShutdownFlags(f)
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
	host    string
	port    int
	gs      *cs.GracefulServer
	r       *gin.Engine
	handler http.Handler
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
	drain := make(chan struct{})
	srv.RegisterOnShutdown(func() { close(drain) })
	srv.BaseContext = func(net.Listener) context.Context { return WithDrain(context.Background(), drain) }
	return s.gs.Serve(srv, ln)
}

type drainKey struct{}

// Draining fires when the server serving ctx's request begins to shut
// down; nil -- never -- outside one. Shutdown waits for every request up to
// WEB_SHUTDOWN_TIMEOUT and does not cancel their contexts, so a long-lived
// stream (the status SSE) held every pod stop for the whole 20 s and was
// cut then anyway: it ends on this instead, and the browser reconnects to
// another pod.
func Draining(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(drainKey{}).(<-chan struct{})
	return ch
}

// WithDrain is ctx with drain as its Draining signal.
func WithDrain(ctx context.Context, drain <-chan struct{}) context.Context {
	return context.WithValue(ctx, drainKey{}, drain)
}

// Close drains the server (cs.GracefulServer): in-flight requests get up to
// WEB_SHUTDOWN_TIMEOUT, long-lived streams are cut after it. Closing the
// listener alone let the process exit mid-response, and the ingress answered
// 502 on every request a terminating pod was still serving.
//
// It must run before the dependencies the handlers use are closed, so
// serve() calls it explicitly rather than leaving it to defer order.
func (s *Web) Close() {
	s.gs.Close()
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
		host: c.String(webHostFlag),
		port: c.Int(webPortFlag),
		gs:   cs.NewGracefulServer(cs.ShutdownTimeout(c)),
		r:    r,
	}, nil
}
