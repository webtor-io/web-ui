package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	csrf "github.com/utrack/gin-csrf"
	"github.com/webtor-io/web-ui/services/common"
)

// The session cookie's mode follows the scheme of DOMAIN. An http instance
// opened by LAN address must get a cookie the browser keeps on plain http
// (no Secure), or every CSRF-checked form answers 400 (self-hosted#116); an
// https one keeps SameSite=None+Secure so a cross-site embed still has a
// session.
func TestSessionCookieFollowsDomainScheme(t *testing.T) {
	// Release, as in every real deployment: the old gin.IsDebugging() gate
	// hid #116 in tests, which run in gin's default debug mode.
	prev := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(prev)

	for _, tc := range []struct {
		domain   string
		secure   bool
		sameSite http.SameSite
	}{
		{"http://localhost:8080", false, http.SameSiteLaxMode},
		{"http://192.168.1.50:8080", false, http.SameSiteLaxMode},
		{"https://webtor.io", true, http.SameSiteNoneMode},
		{"HTTPS://Example.com", true, http.SameSiteNoneMode},
	} {
		t.Run(tc.domain, func(t *testing.T) {
			app := cli.NewApp()
			app.Flags = RegisterFlags([]cli.Flag{
				cli.StringFlag{Name: common.SessionSecretFlag},
				cli.StringFlag{Name: common.DomainFlag},
			})
			r := gin.New()
			app.Action = func(c *cli.Context) error {
				return RegisterHandler(c, r, nil)
			}
			if err := app.Run([]string{"x", "--" + common.SessionSecretFlag, "0123456789abcdef0123456789abcdef",
				"--" + common.DomainFlag, tc.domain}); err != nil {
				t.Fatal(err)
			}
			r.GET("/", func(c *gin.Context) { c.String(200, "ok") })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			var got *http.Cookie
			for _, ck := range w.Result().Cookies() {
				if ck.Name == "session" {
					got = ck
				}
			}
			if got == nil {
				t.Fatalf("no session cookie set: %v", w.Header().Values("Set-Cookie"))
			}
			if got.Secure != tc.secure || got.SameSite != tc.sameSite || !got.HttpOnly {
				t.Fatalf("session cookie Secure=%v SameSite=%v HttpOnly=%v, want Secure=%v SameSite=%v HttpOnly=true",
					got.Secure, got.SameSite, got.HttpOnly, tc.secure, tc.sameSite)
			}
		})
	}
}

// A page keeps the CSRF token it was rendered with. Once the session behind it
// is gone (the cookie expired) or replaced by another one, every later write
// from that tab got the same 400 until a reload; the 400 now carries the
// current token, and the next write that uses it passes.
func TestCSRFMismatchHandsBackCurrentToken(t *testing.T) {
	prev := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(prev)

	app := cli.NewApp()
	app.Flags = RegisterFlags([]cli.Flag{
		cli.StringFlag{Name: common.SessionSecretFlag},
		cli.StringFlag{Name: common.DomainFlag},
	})
	r := gin.New()
	app.Action = func(c *cli.Context) error { return RegisterHandler(c, r, nil) }
	if err := app.Run([]string{"x", "--" + common.SessionSecretFlag, "0123456789abcdef0123456789abcdef",
		"--" + common.DomainFlag, "https://webtor.io"}); err != nil {
		t.Fatal(err)
	}
	r.GET("/", func(c *gin.Context) { c.String(200, csrf.GetToken(c)) })
	r.PUT("/watch/position", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	page := func() (string, *http.Cookie) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		return w.Body.String(), sessionCookie(t, w)
	}
	put := func(token string, ck *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/watch/position", nil)
		req.Header.Set("X-CSRF-TOKEN", token)
		if ck != nil {
			req.AddCookie(ck)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	stale, _ := page()
	_, other := page()
	for name, ck := range map[string]*http.Cookie{"cookie expired": nil, "session replaced": other} {
		t.Run(name, func(t *testing.T) {
			w := put(stale, ck)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("stale token: got %d, want 400", w.Code)
			}
			fresh := w.Header().Get(CSRFTokenHeader)
			if fresh == "" || fresh == stale {
				t.Fatalf("400 carries token %q, want a new one", fresh)
			}
			// No cookie came in, so the 400 had to start the session the
			// fresh token belongs to; otherwise keep the one that came in.
			if ck == nil {
				ck = sessionCookie(t, w)
			}
			if w2 := put(fresh, ck); w2.Code != http.StatusNoContent {
				t.Fatalf("retry with the handed-back token: got %d, want 204", w2.Code)
			}
		})
	}
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "session" {
			return ck
		}
	}
	t.Fatalf("no session cookie set: %v", w.Header().Values("Set-Cookie"))
	return nil
}
