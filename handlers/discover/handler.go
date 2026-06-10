package discover

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// addonView is the per-addon shape we serialize into the page bootstrap
// (window._addons). Carries the manifest snapshot we captured at add
// time so the JS client can render names + capabilities in the
// AddonHealthChip and CatalogSelector before manifests are fetched. The
// JS client lazily refreshes the snapshot via /stremio/addon-url/:id/
// refresh-snapshot when it sees a fresh manifest from an addon whose
// snapshot is missing or older than 7 days.
type addonView struct {
	ID         string     `json:"id"`
	URL        string     `json:"url"`
	Name       string     `json:"name,omitempty"`
	Logo       string     `json:"logo,omitempty"`
	ManifestID string     `json:"manifestId,omitempty"`
	Version    string     `json:"version,omitempty"`
	Resources  []string   `json:"resources,omitempty"`
	Types      []string   `json:"types,omitempty"`
	FetchedAt  *time.Time `json:"fetchedAt,omitempty"`
}

type indexData struct {
	Addons []addonView
}

// enricher is the slice of enrich.Enricher the localize endpoint needs.
// Kept as an interface so the Level 2 batch worker is testable without a
// real mapper chain.
type enricher interface {
	HasMappers() bool
	LocalizeByID(ctx context.Context, videoID string, ct models.ContentType, lang string) (string, string, error)
}

type Handler struct {
	tb template.Builder[*web.Context]
	pg *cs.PG
	en *enrich.Enricher
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, en *enrich.Enricher) {
	h := &Handler{
		tb: tm.MustRegisterViews("discover/*").WithLayout("main"),
		pg: pg,
		en: en,
	}
	r.GET("/discover", h.index)
	r.POST("/discover/localize", auth.HasAuth, h.localize)
	r.POST("/discover/reviews", auth.HasAuth, h.reviews)
}

func (h *Handler) index(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// Preserve the deep-link query (?id=ttXXXX&type=movie) and lang prefix
		// so a guest landing on /ru/discover?id=… ends up back on the same
		// title in the same language after signing in. The login page renders
		// a contextual info card driven by from=discover.
		lang := i18n.GetLang(c)
		returnURL := i18n.LangPath(lang, "/discover")
		if rq := c.Request.URL.RawQuery; rq != "" {
			returnURL += "?" + rq
		}
		v := url.Values{
			"from":       []string{"discover"},
			"return-url": []string{returnURL},
		}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}

	db := h.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	addons, err := models.GetUserStremioAddonUrls(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get addon urls"))
		return
	}

	views := make([]addonView, len(addons))
	for i, a := range addons {
		views[i] = addonView{
			ID:         a.ID.String(),
			URL:        a.Url,
			Name:       derefStr(a.Name),
			Logo:       derefStr(a.ManifestLogo),
			ManifestID: derefStr(a.ManifestID),
			Version:    derefStr(a.ManifestVersion),
			Resources:  a.ManifestResources,
			Types:      a.ManifestTypes,
			FetchedAt:  a.ManifestFetchedAt,
		}
	}

	h.tb.Build("discover/index").HTML(http.StatusOK, web.NewContext(c).WithData(&indexData{
		Addons: views,
	}))
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
