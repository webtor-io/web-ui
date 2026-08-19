package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
)

// signedIn builds a request carrying an authenticated user, the same way
// has_auth_test.go does. The token query parameter is what makes
// GetUserFromContext resolve the user out of the request context.
func signedIn(method, path string) *http.Request {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	req := httptest.NewRequest(method, path+sep+"token=test", nil)
	return req.WithContext(context.WithValue(req.Context(),
		UserContext{}, &models.User{UserID: uuid.NewV4(), Email: "admin@example.com"}))
}

// navigation marks a request as a browser navigating to a page, which is what
// makes HasAuth redirect instead of answering 401.
func navigation(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	return req
}

// The whole design rests on gin applying Use() only to routes registered
// afterwards: static assets and the login form are registered before this
// middleware goes in, and stay reachable because of it. If that ever stopped
// being true, an unauthenticated visitor would get a login page with no
// stylesheet and no way to log in. Pin it.
func TestUseDoesNotAffectEarlierRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/early", func(c *gin.Context) { c.String(http.StatusOK, "early") })
	r.Use(OnlyAuthorized())
	r.GET("/late", func(c *gin.Context) { c.String(http.StatusOK, "late") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/early", nil))
	if w.Code != http.StatusOK {
		t.Errorf("a route registered before the middleware was gated: got %d, want 200", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/late", nil))
	if w.Code == http.StatusOK {
		t.Error("a route registered after the middleware was not gated")
	}
}

func TestOnlyAuthorizedBlocksAnonymousAndAllowsSignedIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(OnlyAuthorized())

	reached := false
	r.GET("/somehash", func(c *gin.Context) {
		reached = true
		c.String(http.StatusOK, "resource page")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, navigation("/somehash"))
	if reached {
		t.Error("an anonymous browser reached a gated page")
	}
	if w.Code != http.StatusFound {
		t.Errorf("anonymous navigation: got %d, want 302 to the login form", w.Code)
	}

	reached = false
	w = httptest.NewRecorder()
	r.ServeHTTP(w, signedIn(http.MethodGet, "/somehash"))
	if !reached {
		t.Error("a signed-in request was blocked from a gated page")
	}
	if w.Code != http.StatusOK {
		t.Errorf("signed-in: got %d, want 200", w.Code)
	}
}

// The surfaces that authenticate by their own mechanics — the JSON API's key,
// the Stremio addon's token, the S3 signature — must stay reachable, or this
// flag silently breaks every integration the instance has.
func TestOnlyAuthorizedSkipsExemptPrefixes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(OnlyAuthorized("/api/v1", "/stremio", "/s3"))

	for _, path := range []string{
		"/api/v1/resource",
		"/api/v1",
		"/stremio/manifest.json",
		"/s3/bucket/key",
	} {
		r.GET(path, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("exempt path %q was gated: got %d, want 200", path, w.Code)
		}
	}
}

// An exempt prefix must match on a path boundary. Without that, exempting
// "/s3" would also exempt "/s3cret-page", and exempting "/api/v1" would let
// "/api/v1x" through — a gate that can be walked around by picking a route
// name is not a gate.
func TestExemptPrefixesMatchOnPathBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(OnlyAuthorized("/s3", "/api/v1"))

	for _, path := range []string{"/s3cret", "/api/v1x"} {
		r.GET(path, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, navigation(path))
		if w.Code == http.StatusOK {
			t.Errorf("path %q slipped through on a prefix that only shares a substring", path)
		}
	}
}
