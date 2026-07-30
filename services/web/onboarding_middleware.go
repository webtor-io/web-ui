package web

import (
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/onboarding"
)

// OnboardingMiddleware makes the activation checklist available to the navbar
// on every page, not only on the home page where the full card lives.
//
// It registers a resolver and does no work itself. That is deliberate: this is
// mounted globally, so it also runs for poster images, Stremio addon polls,
// WebDAV and every other non-HTML route. Querying here would mean one
// checklist lookup per thumbnail on a library page. The query happens only if
// something renders Context.Onboarding, which in practice means a page with a
// navbar.
//
// It lives here rather than in services/onboarding so that package keeps no
// dependency on the web layer: it stays importable (and testable) on its own,
// and services/web is already the home of Context and its middlewares.
//
// Must be mounted AFTER the auth and claims middlewares — the resolver reads
// the user id to query and the tier to decide which steps are locked.
func OnboardingMiddleware(svc *onboarding.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		SetOnboardingResolver(c, func() *models.OnboardingChecklist {
			return loadOnboarding(svc, c)
		})
		c.Next()
	}
}

func loadOnboarding(svc *onboarding.Service, c *gin.Context) *models.OnboardingChecklist {
	u := auth.GetUserFromContext(c)
	if u == nil || !u.HasAuth() {
		return nil
	}
	// Accounts past the activation window never reach the database — that is
	// most signed-in traffic, and the age comes free from the user row the
	// session already carries. A zero CreatedAt means "unknown" rather than
	// "ancient", so it falls through to the query.
	if age := u.CreatedAt; !age.IsZero() && time.Since(age) >= onboarding.ActivationWindow {
		return nil
	}
	cl, err := svc.Get(c.Request.Context(), u.ID, onboarding.PaidTier(c, claims.GetFromContext(c)), time.Now())
	if err != nil {
		// Debug, not Warn: this runs per page render for a slice of users, and
		// a persistent failure would otherwise flood the logs and drown the
		// real error counts.
		log.WithError(err).Debug("failed to build onboarding checklist")
		return nil
	}
	return cl
}
