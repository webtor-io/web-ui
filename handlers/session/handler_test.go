package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
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
