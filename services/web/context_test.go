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

// TestContextCarriesVaultAvailability pins Context.Vault's zero value to
// false: the navbar link and the /vault routes must agree (routes are
// registered only when the vault API client exists), so a field that
// defaulted to true would put the 404 back for any caller that forgets to
// set it.
func TestContextCarriesVaultAvailability(t *testing.T) {
	c := Context{Vault: true}
	if !c.Vault {
		t.Fatal("Context.Vault should round-trip true")
	}
	var zero Context
	if zero.Vault {
		t.Fatal("Context.Vault must default to false, so a caller that forgets to set it hides the link rather than showing a broken one")
	}
}
