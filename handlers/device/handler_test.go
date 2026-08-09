package device

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
)

// The prefix check on revoke is the entire boundary between "remove a device"
// and "remove the account's api/webdav/Stremio token by name" — this test pins
// that a non-device name never reaches the delete.
func TestRevokeRejectsNonDeviceTokenNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{},
			&models.User{UserID: uuid.NewV4(), Email: "u@example.com"})
		c.Request = c.Request.WithContext(ctx)
	})
	h := &Handler{pg: &cs.PG{}}
	r.POST("/device/revoke", h.revoke)

	post := func(name string) int {
		form := url.Values{"name": {name}}
		req := httptest.NewRequest("POST", "/device/revoke?token="+uuid.NewV4().String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	for _, name := range []string{"api", "webdav", "stremio", "", "device"} {
		if code := post(name); code != 400 {
			t.Errorf("revoke(%q): got %d, want 400 — non-device names must never reach the delete", name, code)
		}
	}
	// A device-prefixed name passes the guard and proceeds (dying on the
	// absent test DB, which is fine — the guard is what is under test).
	if code := post(libapi.DeviceTokenPrefix + "cli · F7KQ-29XD"); code == 400 {
		t.Errorf("a legitimate device name was rejected by the guard")
	}
}
