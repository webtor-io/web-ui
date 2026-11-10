package resource

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// A magnet's tr= values are tracker URLs, and a private tracker's passkey is
// in them. A query the form refuses becomes an error that RedirectWithError
// logs; the error says how long the query was, never what it was.
func TestBindArgs_RefusedQueryKeepsNoTracker(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	const magnet = "magnet:?xt=urn:btih:5e4bd524&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey
	gin.SetMode(gin.TestMode)

	form := func() *http.Request {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		_ = mw.WriteField("resource", magnet)
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return r
	}
	for name, req := range map[string]*http.Request{
		"the form":         form(),
		"the magnet route": httptest.NewRequest(http.MethodGet, "/"+magnet, nil),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req
			_, err := (&Handler{}).bindArgs(c)
			if err == nil {
				t.Fatal("the broken magnet was accepted")
			}
			msg := err.Error()
			if strings.Contains(msg, passkey) || strings.Contains(msg, "tracker.invalid") {
				t.Errorf("the error quotes the query: %s", msg)
			}
			if !strings.Contains(msg, "query_len=") || !strings.Contains(msg, "wrong resource provided") {
				t.Errorf("the error lost its shape: %s", msg)
			}
			// The magnet route's query is path + RawQuery, without the "?":
			// only the form's length is the magnet's own.
			if name == "the form" && !strings.Contains(msg, "query_len="+strconv.Itoa(len(magnet))) {
				t.Errorf("want query_len=%d in %s", len(magnet), msg)
			}
		})
	}
}
