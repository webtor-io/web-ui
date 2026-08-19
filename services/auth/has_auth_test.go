package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
)

// HasAuth guards 25 route groups — the profile forms, the whole Discover
// JSON surface, Vault, the Stremio addon URL endpoints. It has to actually
// stop the chain: gin runs its handlers in a loop, and a middleware that
// returns without aborting simply hands over to the next one, which then
// overwrites the 401 with its own answer and runs with an empty user.
func TestHasAuthAbortsAnonymousRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	reached := false
	gr := r.Group("/guarded")
	gr.Use(HasAuth)
	gr.GET("", func(c *gin.Context) {
		reached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/guarded", nil))

	if reached {
		t.Error("the guarded handler ran for an anonymous request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401 (body %q)", w.Code, w.Body.String())
	}
}

func TestHasAuthLetsSignedInRequestsThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	reached := false
	gr := r.Group("/guarded")
	gr.Use(HasAuth)
	gr.GET("", func(c *gin.Context) {
		reached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// A user resolves from the request context once a token parameter is
	// present — the same path the addon endpoints take.
	req := httptest.NewRequest(http.MethodGet, "/guarded?token=test", nil)
	req = req.WithContext(context.WithValue(req.Context(),
		UserContext{}, &models.User{UserID: uuid.NewV4(), Email: "viewer@example.com"}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !reached {
		t.Error("a signed-in request was blocked")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

// A browser navigating to a protected page must land on the login form, not on
// a blank 401, and must be handed back to the page it wanted via the same
// return-url convention every other login entry point already uses
// (handlers/vault/index.go, handlers/discover/handler.go). Everything else —
// XHR, the JSON API, SSE, a same-origin script fetch that happens to accept
// HTML, and a non-browser client like the Stremio addon — must keep getting
// 401, because a 302 to an HTML page is unparseable for them.
func TestHasAuthRedirectsBrowsersButNotAPIClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// A wildcard route so the hostile-path case below (a path the router
	// wouldn't otherwise have registered) still reaches HasAuth with its raw,
	// unnormalized form intact.
	r.Use(HasAuth)
	r.GET("/*path", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	cases := map[string]struct {
		path         string // defaults to "/guarded"
		headers      map[string]string
		wantCode     int
		wantLocation string // checked verbatim when wantCode is StatusFound
	}{
		"browser navigation": {
			headers:      map[string]string{"Accept": "text/html,application/xhtml+xml", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login?return-url=%2Fguarded",
		},
		"xhr": {
			headers:  map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest"},
			wantCode: http.StatusUnauthorized,
		},
		"api client": {
			headers:  map[string]string{"Accept": "application/json"},
			wantCode: http.StatusUnauthorized,
		},
		"html fetch that is not a navigation": {
			headers:  map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "cors"},
			wantCode: http.StatusUnauthorized,
		},
		// The app's own fetch helpers always pair X-Requested-With with a
		// non-HTML Accept, so this combination shouldn't occur in practice —
		// but it pins the ordering in isNavigation: XMLHttpRequest must win
		// over an HTML Accept, not the other way around.
		"xhr that also accepts html still gets 401": {
			headers:  map[string]string{"Accept": "text/html", "X-Requested-With": "XMLHttpRequest"},
			wantCode: http.StatusUnauthorized,
		},
		// Stremio's HTTP stack sends no Sec-Fetch-Mode and no fixture exists
		// for its real Accept header; a wildcard Accept is the common
		// non-browser default and must not be misclassified as a navigation.
		"non-browser client with wildcard accept gets 401": {
			headers:  map[string]string{"Accept": "*/*"},
			wantCode: http.StatusUnauthorized,
		},
		// templates/partials/auth/form.html renders return-url straight into
		// an href with no escaping. Go keeps "//evil.com/x" verbatim in
		// r.URL.Path (it is not normalized into a host), and a leading "//"
		// in an href is a protocol-relative URL browsers resolve off-site —
		// so a path shaped like this must never reach return-url.
		"protocol-relative path is not echoed back as return-url": {
			path:         "//evil.com/x",
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		"triple-slash path is not echoed back as return-url": {
			path:         "///evil.com",
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		// WHATWG URL parsing normalizes a leading backslash to a forward
		// slash for special schemes when a browser resolves an href, so
		// "/\evil.com/x" resolves exactly like "//evil.com/x" — off-site —
		// even though Go's net/url keeps the backslash verbatim in
		// r.URL.Path and never treats it as a host separator itself.
		"backslash-led path is not echoed back as return-url": {
			path:         `/\evil.com/x`,
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		"double-backslash-led path is not echoed back as return-url": {
			path:         `/\evil.com`,
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		// Browsers strip ASCII control characters while parsing a URL, so a
		// path carrying a raw control byte collapses to something else
		// entirely by the time a browser dereferences it — the same
		// stripping that turns a crafted "/\t/evil.com" into "//evil.com",
		// an off-site protocol-relative link. Reject any control character
		// outright rather than trying to enumerate every shape stripping
		// can produce.
		"path with an embedded tab is not echoed back as return-url": {
			path:         "/ok%09/evil.com", // decodes to "/ok\t/evil.com"
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		"path with an embedded newline is not echoed back as return-url": {
			path:         "/ok%0aevil", // decodes to "/ok\nevil"
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login",
		},
		// Legitimate site-relative paths must still round-trip.
		"plain path round-trips as return-url": {
			path:         "/profile",
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login?return-url=%2Fprofile",
		},
		"unicode path round-trips as return-url": {
			path:         "/lib/caf%C3%A9", // decodes to "/lib/café"
			headers:      map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"},
			wantCode:     http.StatusFound,
			wantLocation: "/login?return-url=%2Flib%2Fcaf%C3%A9",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = "/guarded"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusFound {
				if loc := w.Header().Get("Location"); loc != tc.wantLocation {
					t.Errorf("Location: got %q, want %q", loc, tc.wantLocation)
				}
			}
		})
	}
}

// isSafeReturnPath is the gatekeeper for what HasAuth is willing to echo
// back as return-url; the table above already proves it end-to-end through
// the router for the shapes reachable via a real request, but a few shapes
// (an empty path) can't occur through net/http's request parsing at all —
// r.URL.Path is never empty for a request that reached a route — yet the
// function should still refuse them defensively. Testing it directly here
// also makes the exact boundary (which byte failed, and why) easier to see
// than reading it back out of a redirect Location.
func TestIsSafeReturnPath(t *testing.T) {
	cases := map[string]struct {
		path string
		want bool
	}{
		"plain path":                   {"/profile", true},
		"root":                         {"/", true},
		"unicode path":                 {"/lib/café", true},
		"path with query-ish segment":  {"/lib/1?x=1", true},
		"empty path":                   {"", false},
		"protocol-relative (//)":       {"//evil.com/x", false},
		"triple slash (///)":           {"///evil.com", false},
		"single backslash after slash": {`/\evil.com/x`, false},
		"double backslash after slash": {`/\\evil.com`, false},
		"embedded tab before a slash":  {"/ok\t/evil.com", false},
		"embedded newline":             {"/ok\nevil", false},
		"embedded carriage return":     {"/ok\revil", false},
		"no leading slash":             {"evil.com", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isSafeReturnPath(tc.path); got != tc.want {
				t.Errorf("isSafeReturnPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
