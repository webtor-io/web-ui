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
