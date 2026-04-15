package web

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	webHostFlag = "host"
	webPortFlag = "port"
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
	)
}

type Web struct {
	host    string
	port    int
	ln      net.Listener
	r       *gin.Engine
	handler http.Handler
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	s.ln = ln
	if err != nil {
		return errors.Wrap(err, "failed to web listen to tcp connection")
	}
	log.Infof("serving web at %v", addr)
	h := s.handler
	if h == nil {
		h = s.r
	}
	return http.Serve(s.ln, h)
}

func (s *Web) Close() {
	log.Info("closing web")
	defer func() {
		log.Info("web closed")
	}()
	if s.ln != nil {
		_ = s.ln.Close()
	}
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
		r:    r,
	}, nil
}
