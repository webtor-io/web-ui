package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRedirectNonCanonical(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RedirectNonCanonical("https://stage-x7q.webtor.cc", "https://webtor.io"))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "home") })
	r.GET("/foo", func(c *gin.Context) { c.String(http.StatusOK, "foo") })

	cases := []struct {
		host         string
		uri          string
		wantStatus   int
		wantLocation string
	}{
		{"stage-x7q.webtor.cc", "/", http.StatusOK, ""},
		{"STAGE-X7Q.webtor.cc", "/foo", http.StatusOK, ""},
		{"stage-x7q.webtor.cc:443", "/foo", http.StatusOK, ""},
		{"webtor.cc", "/", http.StatusFound, "https://webtor.io/"},
		{"webtor.cc", "/foo?bar=1&baz=2", http.StatusFound, "https://webtor.io/foo?bar=1&baz=2"},
		{"anything.webtor.cc", "/foo", http.StatusFound, "https://webtor.io/foo"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.uri, nil)
		req.Host = tc.host
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Errorf("host %q uri %q: status = %d, want %d", tc.host, tc.uri, w.Code, tc.wantStatus)
		}
		if got := w.Header().Get("Location"); got != tc.wantLocation {
			t.Errorf("host %q uri %q: Location = %q, want %q", tc.host, tc.uri, got, tc.wantLocation)
		}
	}
}

func TestRedirectNonCanonicalDisabled(t *testing.T) {
	if RedirectNonCanonical("https://webtor.io", "") != nil {
		t.Error("expected nil middleware when redirect domain is empty")
	}
}
