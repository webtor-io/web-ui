package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/adminauth"
)

// noPasswordRepo is a HashRepo stub that reports no stored hash, so
// adminauth.NewStore("", noPasswordRepo{}) is unconfigured.
type noPasswordRepo struct{}

func (noPasswordRepo) Get(ctx context.Context) (string, error)    { return "", nil }
func (noPasswordRepo) Set(ctx context.Context, hash string) error { return nil }

// withSession wires the same session middleware the app uses, so the tests
// exercise the real cookie plumbing rather than a stub.
func withSession(r *gin.Engine) {
	store := cookie.NewStore([]byte("test secret"))
	r.Use(sessions.Sessions("session", store))
}

// The whole feature must be inert wherever SuperTokens is configured — that is
// production. A regression here would put a login form in front of every
// webtor.io user.
func TestAdminPasswordInertWithSupertokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A password IS configured here. That is the point: the SuperTokens check
	// must win over a perfectly valid password, otherwise dropping the check
	// would go unnoticed.
	a := &Auth{
		hasSupetokens: true,
		adminStore:    adminauth.NewStore("some password", nil),
	}

	if a.adminPasswordActive(nil) {
		t.Error("the admin-password branch activated while SuperTokens is configured")
	}
}

func TestOpenInstanceWhenNoPasswordConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withSession(r)

	// Exercise the real !hasSupetokens branch of RegisterHandler so the
	// IsOpenInstanceContext value actually gets set — reading IsOpenInstance
	// without ever running that middleware would trivially stay false and
	// prove nothing.
	a := &Auth{
		hasSupetokens: false,
		pg:            &cs.PG{},
		adminStore:    adminauth.NewStore("", noPasswordRepo{}),
	}
	a.RegisterHandler(r)

	var open, admin bool
	r.Use(func(c *gin.Context) {
		open = IsOpenInstance(c)
		admin = IsAdmin(c)
	})
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if !open {
		t.Error("IsOpenInstance is false while no password is configured")
	}
	_ = admin
}
