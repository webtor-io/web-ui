package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
)

// TestMakeUserFromContextDoesNotPanicOnTypedNilUser guards the one
// undefended path in the auth stack: createUser (services/auth/auth.go)
// used to fall through to a bare `return` when a third-party sign-in
// yielded an empty email, which leaves a typed-nil *models.User -- not an
// untyped nil -- in the request context under UserContext{}.
// makeUserFromContext's `su, ok := uc.(*models.User)` type assertion
// succeeds for a typed nil (ok is true even though su is nil), so the
// original code went on to dereference su.UserID etc. and panicked.
//
// This test constructs that fallthrough directly, independent of whether
// createUser can currently produce it, so the assertion holds regardless of
// which providers happen to guard the path today.
func TestMakeUserFromContextDoesNotPanicOnTypedNilUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var typedNilUser *models.User // exactly what the bare `return` in createUser used to leave in context

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserContext{}, typedNilUser))

	c := &gin.Context{Request: req}

	u := makeUserFromContext(c)

	if u == nil {
		t.Fatal("makeUserFromContext returned nil")
	}
	if u.HasAuth() {
		t.Errorf("HasAuth() = true for a context that only ever held a nil user")
	}
}
