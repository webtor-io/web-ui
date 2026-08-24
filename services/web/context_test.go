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

// TestNewContextCarriesVaultAvailability pins the wiring between
// SetVaultEnabled and Context.Vault, the same way
// TestNewContextCarriesOpenInstance pins OpenInstance above: nav.html gates
// both Vault link sites (desktop bar and mobile burger) on this field, and
// the middleware in serve.go that calls SetVaultEnabled -- mounted right
// beside the onboarding one, for the same r.Use-ordering reason -- is the
// only place that decides whether vault is actually configured. This proves
// NewContext actually reads the value back out: a silent drop of the
// `Vault: vaultEnabled(c)` line in the constructor would leave the link
// permanently hidden on instances with vault configured (silent feature
// loss) with no test failing anywhere else -- Context{Vault: true} and a
// zero Context both trivially satisfy Go's zero-value rules regardless of
// whether that line exists, so asserting against literals here would not
// catch it.
func TestNewContextCarriesVaultAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name string
		set  bool // whether SetVaultEnabled is called at all
		val  bool
		want bool
	}{
		{name: "enabled", set: true, val: true, want: true},
		{name: "disabled", set: true, val: false, want: false},
		// Nothing set at all -- the state of every request before this
		// middleware runs, and of any caller that forgets to wire it up.
		// The safe default is "link absent", not "link shown and 404s".
		{name: "unset", set: false, want: false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		if tc.set {
			SetVaultEnabled(c, tc.val)
		}

		got := NewContext(c)
		if got.Vault != tc.want {
			t.Errorf("%s: Context.Vault = %v, want %v", tc.name, got.Vault, tc.want)
		}
	}
}

// TestNewContextCarriesUnreadNotifications pins the wiring between
// SetUnreadNotifications and Context.UnreadNotifications, the same way
// TestNewContextCarriesVaultAvailability pins Vault above: the navbar bell
// renders its badge exclusively from this field, and the middleware in
// serve.go that calls SetUnreadNotifications -- mounted beside the vault
// one, for the same r.Use-ordering reason -- is the only place that knows
// both who the request's user is and what notificationStore.CountUnread
// says. This proves NewContext actually reads the value back out: a silent
// drop of the `UnreadNotifications: unreadNotifications(c)` line in the
// constructor would leave the badge permanently at zero with no test
// failing anywhere else.
func TestNewContextCarriesUnreadNotifications(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		val  int
		want int
	}{
		{"set to three", true, 3, 3},
		{"set to zero", true, 0, 0},
		// Unset must mean zero: a caller who forgets renders no badge
		// rather than a wrong one.
		{"never set", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			if tc.set {
				SetUnreadNotifications(c, tc.val)
			}
			if got := NewContext(c).UnreadNotifications; got != tc.want {
				t.Fatalf("UnreadNotifications = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestVaultEnabledIgnoresWrongType exercises the defensive type assertion in
// vaultEnabled: nothing else in this codebase stores a non-bool under
// vaultEnabledContextKey today, but the failed-assertion branch (`b, _ :=
// v.(bool)`) silently yields false rather than panicking, and that direction
// was untested. Confirming it here means a future change that makes the
// assertion panic instead -- or that makes it silently yield true -- shows
// up as a test failure instead of a runtime surprise.
func TestVaultEnabledIgnoresWrongType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(vaultEnabledContextKey, "not-a-bool")

	if got := vaultEnabled(c); got {
		t.Errorf("vaultEnabled() = %v with a non-bool value stored, want false", got)
	}
}
