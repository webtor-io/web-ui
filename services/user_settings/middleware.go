package user_settings

import (
	"context"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

// Middleware loads the authenticated user's settings into the gin
// context so any downstream handler that constructs a web.Context
// gets `c.UserSettings` populated without an extra DB lookup of its
// own. Anonymous requests get the zero-value Default() so templates
// can read the toggle flags without nil-checks.
//
// It also records the language the account is browsing in — see
// rememberLang. Must be mounted AFTER the auth middleware (we need the
// user id) and after the i18n one (we need the resolved language), but
// before any handler that calls web.NewContext.
// settings is what the middleware needs from the service: read the row,
// and store a language when it changes. An interface so the "write only on
// change" rule can be tested without a database — a rule that would
// otherwise regress into one write per request unnoticed.
type settings interface {
	Get(ctx context.Context, userID uuid.UUID) (*models.UserSettings, error)
	SetLang(ctx context.Context, userID uuid.UUID, lang string) error
}

func Middleware(svc settings) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := auth.GetUserFromContext(c)
		if u != nil && u.HasAuth() {
			if us, err := svc.Get(c.Request.Context(), u.ID); err == nil && us != nil {
				web.SetUserSettings(c, us)
				rememberLang(c, svc, u, us)
			}
		}
		c.Next()
	}
}

// rememberLang stores the language of the current request when it differs
// from what the account already has.
//
// Everywhere else the language lives in the URL prefix and the lang cookie,
// and neither reaches the cron job that sends notifications. This is the one
// place that sees both an authenticated user and a resolved language on
// every page, so it is where the account's language gets learned.
//
// The write is guarded by a comparison, not by a timer: languages change
// when someone uses the switcher, which is roughly never, so the steady
// state costs one string compare per request and no query at all.
func rememberLang(c *gin.Context, svc settings, u *auth.User, us *models.UserSettings) {
	// Only requests that actually went through language routing carry a
	// signal worth storing. The dedicated API hosts skip it and would
	// otherwise report English for every account that uses the API.
	if !i18n.Routed(c) {
		return
	}
	lang := i18n.GetLang(c)
	if lang == "" || lang == us.GetLang() {
		return
	}
	if err := svc.SetLang(c.Request.Context(), u.ID, lang); err != nil {
		// Losing this write costs a notification in the previous language,
		// which is not worth failing a page render over.
		log.WithError(err).
			WithField("user_id", u.ID).
			WithField("lang", lang).
			Warn("failed to store account language")
	}
}
