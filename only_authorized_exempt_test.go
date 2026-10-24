package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/auth"
)

// The exempt list is the one place where ONLY_AUTHORIZED can be undone by
// omission, so pin what belongs on it. Most entries are surfaces with their
// own authentication; /donate is the exception and is listed because it is
// meant to be public. Anything not listed is closed, which is the direction a
// mistake should fall.
func TestOnlyAuthorizedExemptListsEverySelfAuthenticatingSurface(t *testing.T) {
	got := strings.Join(onlyAuthorizedExempt(), " ")

	for _, want := range []string{"/api/v1", "/stremio", "/s3", "/embed", "/donate", "/profile/email/verify", "/user-subtitle/file"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from the exempt list (%s); it authenticates by its own means and would break", want, got)
		}
	}
	// The verification link opens a subtree of /profile, so make it explicit
	// that the rest of the page did not come with it: an entry of "/profile"
	// here would exempt the whole account surface, which is the mistake this
	// assertion exists to catch.
	for _, mustNot := range onlyAuthorizedExempt() {
		if mustNot == "/profile" {
			t.Error("/profile itself is exempt; only /profile/email/verify may be")
		}
	}
	if len(onlyAuthorizedExempt()) != 7 {
		t.Errorf("the exempt list has %d entries (%s); every addition widens what an unauthenticated visitor reaches, so it needs a reason here",
			len(onlyAuthorizedExempt()), got)
	}
}

// external-proxy fetches an uploaded subtitle with a bare GET: no session, no
// Accept header. It must get the file, and upload must stay gated.
func TestOnlyAuthorizedLetsExternalProxyFetchUserSubtitles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(auth.OnlyAuthorized(onlyAuthorizedExempt()...))
	r.GET("/user-subtitle/file/:hash/*name", func(c *gin.Context) { c.String(http.StatusOK, "WEBVTT") })
	r.POST("/user-subtitle", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/user-subtitle/file/0f3a/movie.en.srt", nil))
	if w.Code != http.StatusOK {
		t.Errorf("sessionless GET of a subtitle file: got %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/user-subtitle", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("sessionless upload: got %d, want 401", w.Code)
	}
}
