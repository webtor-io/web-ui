package discover

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
)

// maxLocalizeIDs bounds a single batch so one request can't fan out into
// hundreds of TMDB calls. A Cinemeta catalog page is 50 items, so 100
// covers a page plus the open modal with headroom.
const maxLocalizeIDs = 100

// localizeConcurrency bounds parallel mapper calls per request. First-seen
// ids cost up to 4 TMDB round-trips each (find + details + external ids +
// localized details); repeat sightings are served from tmdb.localized.
const localizeConcurrency = 8

type localizeRequest struct {
	Items []localizeRequestItem `json:"items"`
}

type localizeRequestItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type localizedItem struct {
	Title string `json:"title,omitempty"`
	Plot  string `json:"plot,omitempty"`
}

type localizeResponse struct {
	Items map[string]localizedItem `json:"items"`
}

// localize is the Level 1 handler for POST /discover/localize. It returns
// localized title/plot for a batch of catalog video ids in the request
// language. English requests short-circuit to an empty map — the client
// doesn't call this for "en", but the guard keeps the contract honest.
func (h *Handler) localize(c *gin.Context) {
	if h.en == nil || !h.en.HasMappers() {
		c.JSON(http.StatusOK, localizeResponse{Items: map[string]localizedItem{}})
		return
	}
	var req localizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}
	lang := i18n.GetLang(c)
	items := localizeItems(c.Request.Context(), h.en, lang, req.Items)
	c.JSON(http.StatusOK, localizeResponse{Items: items})
}

// localizeItems is the Level 2 worker: filters the batch down to ids the
// enrichment pipeline can handle, then fans out LocalizeByID calls with
// bounded concurrency. Three outcomes per id, and the response encodes the
// difference: localized → {title, plot}; checked-but-no-translation →
// explicit empty object (the client may cache the negative verdict);
// pipeline error → id omitted entirely (the client must NOT cache it, a
// later batch retries).
func localizeItems(ctx context.Context, en enricher, lang string, reqItems []localizeRequestItem) map[string]localizedItem {
	out := make(map[string]localizedItem)
	if lang == "en" {
		return out
	}

	type target struct {
		id string
		ct models.ContentType
	}
	seen := make(map[string]struct{})
	var targets []target
	for _, it := range reqItems {
		if len(targets) == maxLocalizeIDs {
			break
		}
		if !localizableID(it.ID) {
			continue
		}
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		ct := models.ContentTypeMovie
		if it.Type == "series" {
			ct = models.ContentTypeSeries
		}
		targets = append(targets, target{id: it.ID, ct: ct})
	}

	results := make([]localizedItem, len(targets))
	failed := make([]bool, len(targets))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(localizeConcurrency)
	for i, t := range targets {
		g.Go(func() error {
			title, plot, err := en.LocalizeByID(gctx, t.id, t.ct, lang)
			results[i] = localizedItem{Title: title, Plot: plot}
			failed[i] = err != nil && title == "" && plot == ""
			return nil
		})
	}
	_ = g.Wait()

	for i, t := range targets {
		if failed[i] {
			continue
		}
		out[t.id] = results[i]
	}
	return out
}

// localizableID accepts bare IMDB title ids — the shape Stremio catalogs
// carry and the only id format whose type the pipeline can verify
// (an IMDB id maps to exactly one TMDB entity). Bare tmdb* numeric ids are
// rejected: their movie/tv namespaces overlap, so a client-supplied type
// hint could make MapByID upsert a wrong-namespace title into tmdb.info.
// Episode ids (tt123:1:2) and custom addon ids are skipped client-side
// too; this is the server-side guard.
func localizableID(id string) bool {
	if strings.Contains(id, ":") {
		return false
	}
	return strings.HasPrefix(id, "tt")
}
