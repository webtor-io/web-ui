package user_settings

import (
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// Middleware loads the authenticated user's settings into the gin
// context so any downstream handler that constructs a web.Context
// gets `c.UserSettings` populated without an extra DB lookup of its
// own. Anonymous requests get the zero-value Default() so templates
// can read the toggle flags without nil-checks.
//
// Should be mounted AFTER the auth middleware (we need the user id)
// but before any handler that calls web.NewContext.
func Middleware(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := auth.GetUserFromContext(c)
		if u != nil && u.HasAuth() {
			if us, err := svc.Get(c.Request.Context(), u.ID); err == nil && us != nil {
				web.SetUserSettings(c, us)
			}
		}
		c.Next()
	}
}
