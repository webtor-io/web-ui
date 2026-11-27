package resource

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/web"
)

// A magnet's tr= and as= values are tracker and download URLs, and a private
// tracker's key is in them. A query the form refuses becomes an error that
// RedirectWithError logs: the query's length, and the query without them
// (common.RefusedQuery), so a refused title can still be read.
func TestBindArgs_RefusedQueryKeepsNoTracker(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	const magnet = "magnet:?xt=urn:btih:5e4bd524&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey + "&as=https%3A%2F%2Ftracker.invalid%2F1." + passkey
	gin.SetMode(gin.TestMode)

	form := func(q string) *http.Request {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		_ = mw.WriteField("resource", q)
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return r
	}
	for _, tc := range []struct {
		name string
		req  *http.Request
		has  []string
	}{
		{"the form", form(magnet), []string{"query_len=" + strconv.Itoa(len(magnet)), "xt=urn:btih:5e4bd524&tr=<redacted>&as=<redacted>"}},
		// The magnet route's query is path + RawQuery, without the "?".
		{"the magnet route", httptest.NewRequest(http.MethodGet, "/"+magnet, nil), []string{"xt=urn:btih:5e4bd524&tr=<redacted>&as=<redacted>"}},
		{"free text", form("Big Buck Bunny 2008"), []string{`query_len=19 query="Big Buck Bunny 2008"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = tc.req
			_, err := (&Handler{}).bindArgs(c)
			if err == nil {
				t.Fatal("the query was accepted")
			}
			msg := err.Error()
			if strings.Contains(msg, passkey) || strings.Contains(msg, "tracker.invalid") {
				t.Errorf("the error quotes the trackers: %s", msg)
			}
			for _, w := range append(tc.has, "wrong resource provided query_len=") {
				if !strings.Contains(msg, w) {
					t.Errorf("%s\nlacks %s", msg, w)
				}
			}
		})
	}
}

// A client gone before its job is queued gets neither a job nor a redirect:
// web.ErrorHandler records the request as 499.
func TestPostClientGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(web.ErrorHandler(nil)) // renders nothing for a client that left
	// Load returns before it touches the queue, so an empty Jobs does.
	r.GET("/*path", (&Handler{jobs: &j.Jobs{}}).post)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/magnet:?xt=urn:btih:"+strings.Repeat("a", 40), nil).WithContext(gone))
	if rec.Code != web.StatusClientClosedRequest || rec.Header().Get("Location") != "" {
		t.Errorf("got %d, Location %q; want 499 and no redirect", rec.Code, rec.Header().Get("Location"))
	}
}
