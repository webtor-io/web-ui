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

func TestRedactURL(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"the addon's token and a resolve link",
			"/token/" + logAddonUUID + "/stremio/resolve/" + logJWT,
			"/token/<redacted>/stremio/resolve/<redacted>"},
		{"a stream token in the query",
			"/stremio/resolve/" + logJWT + "?token=" + logJWT,
			"/stremio/resolve/<redacted>?token=<redacted>"},
		{"a key and a token, encoded in a URL passed on",
			"/x?u=%2Fa.mkv%3Fapi-key%3Dk-12345678%26token%3D" + logJWT + "%26download%3Dtrue",
			"/x?u=%2Fa.mkv%3Fapi-key%3D<redacted>%26token%3D<redacted>%26download%3Dtrue"},
		{"the unsubscribe link",
			"/subscription/unsubscribe/3f9a1c0e7b2d",
			"/subscription/unsubscribe/<redacted>"},
		{"the email verification link",
			"/profile/email/verify/a1b2c3d4e5?lang=ru",
			"/profile/email/verify/<redacted>?lang=ru"},
		{"names that only look like one",
			"/tokens/abc/mytoken/def/stremio/manifest.json?mytoken=1&tokenizer=2",
			"/tokens/abc/mytoken/def/stremio/manifest.json?mytoken=1&tokenizer=2"},
		{"nothing to hide",
			"/ru/80d7a3c8?file=/Sintel/Sintel.mkv",
			"/ru/80d7a3c8?file=/Sintel/Sintel.mkv"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactURL(c.in); got != c.want {
				t.Errorf("RedactURL(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

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
