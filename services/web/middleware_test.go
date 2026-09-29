package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/services/metrics"
	"github.com/webtor-io/web-ui/services/metrics/metricstest"
	"github.com/webtor-io/web-ui/services/template"
)

// errorRoute mounts the tests' failing chain under the status stream's shape
// (metrics.Streaming first, like handlers/resource), with a series of its own
// in webui_http_requests_total.
const errorRoute = "/error-handler-test/:resource_id/status"

const errorPath = "/error-handler-test/0123456789abcdef0123456789abcdef01234567/status"

// newErrorRouter wires the engine the way serve.go does — gin's logger, the
// metrics middleware, recovery, then ErrorHandler — in front of chain, with a
// stand-in error page that prints the key it was given. The template manager
// resolves "templates/" against the working directory, hence the chdir.
func newErrorRouter(t *testing.T, chain ...gin.HandlerFunc) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"templates/layouts/main.html":     `{{ template "main" . }}`,
		"templates/views/error/page.html": `{{ define "main" }}[error page {{ .ErrKey }}]{{ end }}`,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates/partials"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	gin.SetMode(gin.TestMode)
	var ginLog bytes.Buffer
	r := gin.New()
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*Context](re)
	r.Use(gin.LoggerWithWriter(&ginLog), metrics.Middleware(), gin.CustomRecovery(RecoverToLog))
	r.Use(ErrorHandler(tm.MustRegisterViews("error/*").WithLayout("main")))
	r.GET(errorRoute, append([]gin.HandlerFunc{metrics.Streaming}, append(chain, func(c *gin.Context) {
		c.String(http.StatusOK, "streamed")
	})...)...)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r, &ginLog
}

// abortWith is a middleware failing the way services/auth does when its user
// lookup fails: the error attached, the chain aborted, nothing written.
func abortWith(err error) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.Error(err)
		c.Abort()
	}
}

// clientLeft is a request whose client has gone: net/http cancels the
// request's context when the connection closes.
func clientLeft(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	return req.WithContext(ctx)
}

func requests(t *testing.T, status string) float64 {
	t.Helper()
	return metricstest.Counter(t, "webui_http_requests_total", map[string]string{
		"route": errorRoute, "method": http.MethodGet, "status": status,
	})
}

func entries(hook *test.Hook, level log.Level) []*log.Entry {
	var out []*log.Entry
	for _, e := range hook.AllEntries() {
		if e.Level == level {
			out = append(out, e)
		}
	}
	return out
}

// A viewer closing the page while its status streams are still passing the
// middleware (2026-09-27 18:16:36Z: 33 streams, one client, one second) must
// not read as a server failure: no error page, no error-level line, and 499
// — not 500 — in gin's log and in webui_http_requests_total.
func TestErrorHandler_ClientGoneIsNotAServerError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		// go-pg's pool wait on a done context returns ctx.Err() bare. So
		// does its retry sleep, in place of the failure it was retrying:
		// that one reads as the client leaving too (clientGone's blind
		// spot, docs/user_errors.md).
		{"bare", context.Canceled},
		{"wrapped", errors.Wrap(context.Canceled, "failed to get user")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := test.NewGlobal()
			defer hook.Reset()
			r, ginLog := newErrorRouter(t, abortWith(tc.err))
			b499, b500 := requests(t, "499"), requests(t, "500")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, clientLeft(errorPath))

			if w.Code != StatusClientClosedRequest {
				t.Errorf("status %d, want %d", w.Code, StatusClientClosedRequest)
			}
			if w.Body.Len() != 0 {
				t.Errorf("body %q: nothing is rendered for a client that left", w.Body.String())
			}
			if got := requests(t, "499") - b499; got != 1 {
				t.Errorf("requests_total{status=499} +%v, want +1", got)
			}
			if got := requests(t, "500") - b500; got != 0 {
				t.Errorf("requests_total{status=500} +%v, want +0", got)
			}
			if !strings.Contains(ginLog.String(), "| 499 |") {
				t.Errorf("gin log %q, want status 499", ginLog.String())
			}
			if errs := entries(hook, log.ErrorLevel); len(errs) != 0 {
				t.Errorf("error-level lines %d, first %q: a client leaving is not an error", len(errs), errs[0].Message)
			}
			var closed int
			for _, e := range hook.AllEntries() {
				if e.Message == "user error shown" {
					t.Errorf("%q logged: no page was shown", e.Message)
				}
				if e.Message == "client closed request" {
					closed++
					if e.Level != log.InfoLevel || e.Data["status"] != StatusClientClosedRequest {
						t.Errorf("client closed request: level %v status %v, want info 499", e.Level, e.Data["status"])
					}
				}
			}
			if closed != 1 {
				t.Errorf("%d \"client closed request\" lines, want 1", closed)
			}
		})
	}
}

// Everything that is not the client leaving stays what it was: the page and
// a 500, logged as an error. Each case keeps one half of clientGone true.
func TestErrorHandler_ServerErrorsStay500(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain gin.HandlerFunc
		req   func() *http.Request
	}{
		{
			// A cancel of the chain's own, with the client still connected:
			// the middleware replaced the request's context with one it
			// cancelled. Read after c.Next(), c.Request's context would look
			// like a client that left.
			name: "our own cancel, client connected",
			chain: func(c *gin.Context) {
				ctx, cancel := context.WithCancel(c.Request.Context())
				c.Request = c.Request.WithContext(ctx)
				cancel()
				_ = c.Error(ctx.Err())
				c.Abort()
			},
			req: func() *http.Request { return httptest.NewRequest(http.MethodGet, errorPath, nil) },
		},
		{
			// The failure came first; the client left before the answer.
			// go-pg does not retry a pool timeout (it has no Timeout
			// method), so it reaches the handler as itself. A reset or
			// refused connection, which go-pg retries, does not: a client
			// leaving during the backoff turns it into a bare
			// context.Canceled (the "bare" case of
			// TestErrorHandler_ClientGoneIsNotAServerError).
			name:  "server failure, then the client left",
			chain: abortWith(errors.New("pg: connection pool timeout")),
			req:   func() *http.Request { return clientLeft(errorPath) },
		},
		{
			name:  "timeout, then the client left",
			chain: abortWith(errors.Wrap(context.DeadlineExceeded, "failed to fetch user claims")),
			req:   func() *http.Request { return clientLeft(errorPath) },
		},
		{
			name:  "server failure, client connected",
			chain: abortWith(errors.New("something nobody anticipated")),
			req:   func() *http.Request { return httptest.NewRequest(http.MethodGet, errorPath, nil) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := test.NewGlobal()
			defer hook.Reset()
			r, ginLog := newErrorRouter(t, tc.chain)
			b499, b500 := requests(t, "499"), requests(t, "500")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, tc.req())

			if w.Code != http.StatusInternalServerError {
				t.Errorf("status %d, want 500", w.Code)
			}
			if !strings.Contains(w.Body.String(), "[error page error.generic]") {
				t.Errorf("body %q, want the error page", w.Body.String())
			}
			if got := requests(t, "500") - b500; got != 1 {
				t.Errorf("requests_total{status=500} +%v, want +1", got)
			}
			if got := requests(t, "499") - b499; got != 0 {
				t.Errorf("requests_total{status=499} +%v, want +0", got)
			}
			if !strings.Contains(ginLog.String(), "| 500 |") {
				t.Errorf("gin log %q, want status 500", ginLog.String())
			}
			errs := entries(hook, log.ErrorLevel)
			if len(errs) != 1 || errs[0].Message != "request failed" {
				t.Fatalf("error-level lines %+v, want one \"request failed\"", errs)
			}
		})
	}
}

// A handler that answered already (a redirect, its own status) keeps its
// answer when the client leaves; only the line in the log changes level.
func TestErrorHandler_ClientGoneAfterAnAnswer(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	r, _ := newErrorRouter(t, func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/")
		_ = c.Error(errors.Wrap(context.Canceled, "failed to get resource"))
		c.Abort()
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, clientLeft(errorPath))

	if w.Code != http.StatusFound {
		t.Errorf("status %d, want the handler's 302", w.Code)
	}
	if errs := entries(hook, log.ErrorLevel); len(errs) != 0 {
		t.Errorf("error-level lines %d, first %q: a client leaving is not an error", len(errs), errs[0].Message)
	}
	infos := entries(hook, log.InfoLevel)
	if len(infos) != 1 || infos[0].Message != "client closed request" || infos[0].Data["status"] != http.StatusFound {
		t.Errorf("info lines %+v, want one \"client closed request\" with status 302", infos)
	}
}
