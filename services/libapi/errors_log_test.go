package libapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus/hooks/test"
)

// POST /resource hands a magnet to rest-api as it came, and rest-api answers a
// magnet its parser refuses with the parser's error, which quotes the URI
// whole (url.Parse). The line WriteError logs keeps the error without the
// magnet's trackers -- a private tracker's passkey is part of a tracker URL;
// the client still gets the code and message, never the cause.
func TestWriteError_LogKeepsNoMagnetTrackers(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	hook := test.NewGlobal()
	defer hook.Reset()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/resource", nil)
	cause := errors.New(`failed to parse magnet: parse "magnet:?xt=urn:btih:5e4bd524\n&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D` + passkey + `": net/url: invalid control character in URL`)
	WriteError(c, NewError(http.StatusBadRequest, CodeBadRequest, "the request could not be parsed", cause))

	var lines []string
	for _, e := range hook.AllEntries() {
		s, _ := e.String()
		lines = append(lines, s+fmt.Sprint(e.Data))
	}
	all := strings.Join(lines, "\n")
	if strings.Contains(all, passkey) || strings.Contains(all, "tracker.invalid") {
		t.Errorf("the log keeps the tracker:\n%s", all)
	}
	if !strings.Contains(all, "api request rejected") || !strings.Contains(all, "invalid control character") || !strings.Contains(all, "tr=<redacted>") {
		t.Errorf("the line lost its error:\n%s", all)
	}
	if strings.Contains(w.Body.String(), "magnet:") {
		t.Errorf("the cause reached the client: %s", w.Body.String())
	}
}

// The JSON API takes a resource id in the path the way rest-api does, so a
// client can send a magnet there, escaped: the path WriteError logs is the
// decoded URL.Path, the magnet's trackers and sources in it.
func TestWriteError_PathKeepsNoMagnetTrackers(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	hook := test.NewGlobal()
	defer hook.Reset()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/resource/magnet%3A%3Fxt%3Durn%3Abtih%3A5e4bd524%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252Fannounce%253Fpasskey%253D"+passkey+"%26as%3Dhttps%253A%252F%252Ftracker.invalid%252F1."+passkey, nil)
	WriteError(c, NewError(http.StatusBadRequest, CodeBadRequest, "the request could not be parsed", errors.New("failed to parse magnet")))

	if len(hook.AllEntries()) != 1 {
		t.Fatalf("want one line, got %d", len(hook.AllEntries()))
	}
	path := fmt.Sprint(hook.LastEntry().Data["path"])
	if strings.Contains(path, passkey) || strings.Contains(path, "tracker.invalid") {
		t.Errorf("the path keeps the tracker: %s", path)
	}
	if want := "/api/v1/resource/magnet:?xt=urn:btih:5e4bd524&tr=<redacted>&as=<redacted>"; path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
}
