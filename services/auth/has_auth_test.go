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
