package web

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/auth"
)

// TestNewContextCarriesOpenInstance pins the wiring between
// auth.IsOpenInstance and Context.OpenInstance: the open-instance banner
// (templates/partials/open_instance_banner.html) is the only warning a
// stranger-reachable, password-less self-hosted instance gets, and it reads
// exclusively from this field. auth.TestOpenInstanceWhenNoPasswordConfigured
// already proves the auth package sets the request-context value correctly;
// this proves NewContext actually reads it back out — a silent drop of the
// `OpenInstance: auth.IsOpenInstance(c)` line in the constructor would leave
// the banner permanently unrendered with no test failing anywhere else.
func TestNewContextCarriesOpenInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, open := range []bool{true, false} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/", nil)
		if open {
			req = req.WithContext(context.WithValue(req.Context(), auth.IsOpenInstanceContext{}, true))
		}
		c.Request = req

		got := NewContext(c)
		if got.OpenInstance != open {
			t.Errorf("open=%v: Context.OpenInstance = %v, want %v", open, got.OpenInstance, open)
		}
	}
}
