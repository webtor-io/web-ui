package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus/hooks/test"
)

// A magnet page's address is the magnet, trackers included, and a private
// tracker's passkey is part of a tracker URL. Lines that name the page carry
// it without them.
const (
	logHash    = "08ada5a7a6183aae1e09d831df6748d566095a10"
	logPasskey = "0123456789abcdef0123456789abcdef"
)

func entriesWith(hook *test.Hook, s string) []string {
	var out []string
	for _, e := range hook.AllEntries() {
		line := e.Message + fmt.Sprint(e.Data)
		if strings.Contains(line, s) {
			out = append(out, line)
		}
	}
	return out
}

// The return URL of a form on a magnet page (async.js: pathname + search) is
// that page's magnet. The redirect keeps it -- it is the visitor's own page --
// the log line does not.
func TestRedirectWithError_LogKeepsNoMagnetTrackers(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	gin.SetMode(gin.TestMode)
	page := "/magnet:?xt=urn:btih:" + logHash + "&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + logPasskey
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	RedirectWithErrorAndPath(c, page, errors.New("failed to add torrent to library"))

	if leaked := entriesWith(hook, logPasskey); len(leaked) != 0 {
		t.Errorf("the log keeps the tracker: %q", leaked)
	}
	if got := entriesWith(hook, "tr=<redacted>"); len(got) != 1 {
		t.Errorf("want 1 line with the redacted path, got %q", got)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, logPasskey) {
		t.Errorf("the redirect must go back to the page as it was: %s", loc)
	}
}

// Through the browser's magnet: handler (manifest "/%s") the whole magnet is
// in the path, percent-encoded; Go decodes it into URL.Path, which is what the
// error middleware logs.
func TestErrorHandler_LogKeepsNoMagnetTrackers(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	r, _ := newErrorRouter(t)
	r.GET("/:resource_id", abortWith(errors.New("failed to get claims")))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/magnet%3A%3Fxt%3Durn%3Abtih%3A"+logHash+"%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252F"+logPasskey+"%252Fannounce", nil))

	if leaked := entriesWith(hook, logPasskey); len(leaked) != 0 {
		t.Errorf("the log keeps the tracker: %q", leaked)
	}
	// "request failed" and "user error shown", both with the page.
	if got := entriesWith(hook, "tr=<redacted>"); len(got) != 2 {
		t.Errorf("want 2 lines with the redacted path, got %q", got)
	}
}
