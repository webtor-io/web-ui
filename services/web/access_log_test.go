package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The shapes the access log carried (Loki, 2026-09-30).
const (
	logJWT       = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZvSGFzaCI6ImFiYyJ9.c2lnbmF0dXJlLXNpZ25hdHVyZQ"
	logAddonUUID = "c7b58e23-9cd3-4747-ac94-fe9864f99ead"
)

// The middleware: gin's line, status and all, without the credentials.
func TestAccessLogKeepsNoCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	r := gin.New()
	r.Use(AccessLogTo(&buf))
	r.GET("/token/:id/stremio/resolve/:p", func(c *gin.Context) { c.Status(http.StatusFound) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/token/"+logAddonUUID+"/stremio/resolve/"+logJWT+"?token="+logJWT, nil))
	line := buf.String()
	if strings.Contains(line, logAddonUUID) || strings.Contains(line, logJWT[:40]) {
		t.Fatalf("the line keeps a credential: %s", line)
	}
	for _, want := range []string{"[GIN] ", "| 302 |", `"/token/<redacted>/stremio/resolve/<redacted>?token=<redacted>"`} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
}

// A magnet page's address is the magnet, trackers included, and a private
// tracker's passkey is part of a tracker URL. The three ways a magnet comes
// in as a page: GET /magnet:?…, the same through the browser's magnet:
// handler (manifest "/%s", the whole magnet percent-encoded into the path),
// and the extension's /ext/magnet?url=<magnet>.
func TestAccessLogKeepsNoMagnetTrackers(t *testing.T) {
	const (
		hash    = "08ada5a7a6183aae1e09d831df6748d566095a10"
		passkey = "0123456789abcdef0123456789abcdef"
	)
	gin.SetMode(gin.TestMode)
	for _, c := range []struct{ name, target, want string }{
		{"the magnet route",
			"/magnet:?xt=urn:btih:" + hash + "&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey,
			`"/magnet:?xt=urn:btih:` + hash + `&tr=<redacted>"`},
		{"the protocol handler",
			"/magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252F" + passkey + "%252Fannounce",
			`"/magnet:?xt=urn:btih:` + hash + `&tr=<redacted>"`},
		{"the extension",
			"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252Fannounce%253Fpasskey%253D" + passkey + "&utm_source=ext",
			`"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A` + hash + `%26tr%3D<redacted>&utm_source=ext"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := gin.New()
			r.Use(AccessLogTo(&buf))
			r.GET("/ext/magnet", func(c *gin.Context) { c.Status(http.StatusOK) })
			r.GET("/:resource_id", func(c *gin.Context) { c.Status(http.StatusAccepted) })
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, c.target, nil))
			line := buf.String()
			if strings.Contains(line, passkey) || strings.Contains(line, "tracker.invalid") {
				t.Fatalf("the line keeps a tracker: %s", line)
			}
			if !strings.Contains(line, c.want) {
				t.Errorf("line %q lacks %q", line, c.want)
			}
		})
	}
}
