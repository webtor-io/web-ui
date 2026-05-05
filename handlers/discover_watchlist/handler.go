// Package discover_watchlist exposes per-user "watch later" intent for the
// Discover Preact app. All routes live under /discover/watchlist/* and are
// gated by auth.
//
// HTTP contract
//
//	GET    /discover/watchlist             → 200 { items, video_ids, limit }
//	GET    /discover/watchlist/ids         → 200 { video_ids } (lightweight badge prefetch)
//	POST   /discover/watchlist             → 200 { added } / 402 { code: "limit_exceeded" }
//	DELETE /discover/watchlist/:type/:vid  → 200 { removed }
//
// Status codes
//
//	200 — success
//	400 — bad payload (missing video_id, unknown type)
//	401 — not logged in (auth middleware handles this)
//	402 — free-tier soft cap hit on add
//	500 — DB / enrichment failure
//
// Two-level handler pattern (per CLAUDE.md): the gin handlers below unpack
// requests and map errors to HTTP codes; the do* methods do the DB work and
// return typed errors.
package discover_watchlist

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"

	proto "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
)

// FreeTierWatchlistLimit caps each free user's combined movie + series
// watchlist size. Paid users are unlimited. The number is a soft anti-abuse
// cap, not a billing lever — it exists so a single bad actor can't park
// hundreds of thousands of rows under one account. 200 leaves enough room
// for genuine "I want to watch a lot of stuff" usage while still bounding
// the worst case.
const FreeTierWatchlistLimit = 200

// Sources accepted by POST /discover/watchlist. Validated server-side so
// analytics queries on the `source` column don't get polluted by typos.
var validSources = map[string]struct{}{
	"ai":      {}, // AI recommendations card
	"search":  {}, // search results grid
	"catalog": {}, // catalog browse grid
	"streamy": {}, // stream-pick modal
}

type Handler struct {
	pg *cs.PG
	en *enrich.Enricher // may be nil — feature still works, just no metadata fetch on add
}

func RegisterHandler(r *gin.Engine, pg *cs.PG, en *enrich.Enricher) {
	h := &Handler{pg: pg, en: en}
	gr := r.Group("/discover/watchlist")
	gr.Use(auth.HasAuth)
	gr.GET("", h.list)
	gr.GET("/ids", h.listIDs)
	gr.POST("", h.add)
	gr.DELETE("/:type/:video_id", h.remove)
}

// --- DTOs ---

type addRequest struct {
	VideoID string `json:"video_id"`
	Type    string `json:"type"`
	Source  string `json:"source"`
}

type listResponse struct {
	Items    []models.WatchlistItem `json:"items"`
	VideoIDs []string               `json:"video_ids"`
	Limit    int                    `json:"limit"` // -1 = unlimited (paid)
}

type idsResponse struct {
	VideoIDs []string `json:"video_ids"`
	Limit    int      `json:"limit"`
}

type errorBody struct {
	// Status mirrors the success path so the client's toast wrapper can
	// branch on a single field across all responses (matches the
	// convention in handlers/user_video_status/handler.go).
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Limit   int    `json:"limit,omitempty"`
}

// successBody is the shape POST/DELETE return on success. status + message
// match the server-translated toast convention used by the rest of the
// discover surface (see redirectWithRatePrompt in user_video_status).
// `added` / `removed` carry the action-specific payload.
type successBody struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Added   bool   `json:"added,omitempty"`
	Removed bool   `json:"removed,omitempty"`
}

// --- Level 1: gin handlers ---

func (h *Handler) list(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	db := h.pg.Get()
	if db == nil {
		writeInternal(c, errors.New("no db"))
		return
	}
	items, ids, err := h.doList(c.Request.Context(), db, user.ID)
	if err != nil {
		log.WithError(err).WithField("feature", "watchlist").Error("list failed")
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, listResponse{
		Items:    items,
		VideoIDs: ids,
		Limit:    limitFor(c),
	})
}

func (h *Handler) listIDs(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	db := h.pg.Get()
	if db == nil {
		writeInternal(c, errors.New("no db"))
		return
	}
	ids, err := h.doListIDs(c.Request.Context(), db, user.ID)
	if err != nil {
		log.WithError(err).WithField("feature", "watchlist").Error("list ids failed")
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, idsResponse{VideoIDs: ids, Limit: limitFor(c)})
}

func (h *Handler) add(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	db := h.pg.Get()
	if db == nil {
		writeInternal(c, errors.New("no db"))
		return
	}

	var req addRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Status: "error", Code: "bad_request", Message: i18n.T(c, "discover.watchlist.addFailed")})
		return
	}
	ct, err := parseContentType(req.Type)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Status: "error", Code: "bad_type", Message: i18n.T(c, "discover.watchlist.addFailed")})
		return
	}
	if req.VideoID == "" || !strings.HasPrefix(req.VideoID, "tt") {
		// Discover only renders IMDB-keyed cards; reject anything else early
		// so we don't end up with rows that can't be JOINed against
		// movie_metadata / series_metadata.
		c.JSON(http.StatusBadRequest, errorBody{Status: "error", Code: "bad_video_id", Message: i18n.T(c, "discover.watchlist.addFailed")})
		return
	}
	source := req.Source
	if _, ok := validSources[source]; !ok {
		// Unknown source string is allowed but normalised to "other" so the
		// `source` column stays clean for analytics. Old client + new server
		// shouldn't 400 here.
		source = "other"
	}

	limit := limitFor(c)
	added, err := h.doAdd(c.Request.Context(), db, user.ID, req.VideoID, ct, source, limit)
	if err != nil {
		if errors.Is(err, errLimitExceeded) {
			c.JSON(http.StatusPaymentRequired, errorBody{
				Status:  "error",
				Code:    "limit_exceeded",
				Message: i18n.T(c, "discover.watchlist.limitReached"),
				Limit:   limit,
			})
			return
		}
		log.WithError(err).WithField("feature", "watchlist").Error("add failed")
		writeInternal(c, err)
		return
	}
	// If the row already existed (added=false on conflict) we skip the
	// success message — there's nothing user-visible to celebrate. The
	// client treats an empty message as "don't toast".
	body := successBody{Status: "success", Added: added}
	if added {
		body.Message = i18n.T(c, "discover.watchlist.added")
	}
	c.JSON(http.StatusOK, body)
}

func (h *Handler) remove(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	db := h.pg.Get()
	if db == nil {
		writeInternal(c, errors.New("no db"))
		return
	}

	ct, err := parseContentType(c.Param("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Status: "error", Code: "bad_type", Message: i18n.T(c, "discover.watchlist.removeFailed")})
		return
	}
	videoID := c.Param("video_id")
	if videoID == "" {
		c.JSON(http.StatusBadRequest, errorBody{Status: "error", Code: "bad_video_id", Message: i18n.T(c, "discover.watchlist.removeFailed")})
		return
	}

	if err := h.doRemove(c.Request.Context(), db, user.ID, videoID, ct); err != nil {
		log.WithError(err).WithField("feature", "watchlist").Error("remove failed")
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, successBody{
		Status:  "success",
		Message: i18n.T(c, "discover.watchlist.removed"),
		Removed: true,
	})
}

// writeInternal centralises the 500 branch — Watchlist surfaces never need
// a finer-grained server-error message, so a single localised "couldn't add
// /remove" toast plus a generic "internal" code is enough. The caller logs
// the underlying error.
func writeInternal(c *gin.Context, _ error) {
	// Generic — both add and remove paths share the same toast key for
	// catastrophic failures because the user can't act on the distinction.
	c.JSON(http.StatusInternalServerError, errorBody{
		Status:  "error",
		Code:    "internal",
		Message: i18n.T(c, "discover.somethingWrong"),
	})
}

// --- Level 2: business logic ---

var errLimitExceeded = errors.New("watchlist limit exceeded")

func (h *Handler) doList(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]models.WatchlistItem, []string, error) {
	movies, err := models.ListMovieWatchlistItems(ctx, db, userID)
	if err != nil {
		return nil, nil, err
	}
	series, err := models.ListSeriesWatchlistItems(ctx, db, userID)
	if err != nil {
		return nil, nil, err
	}
	// Merge in stable order: newest-first across both types. Each list comes
	// in already sorted DESC by created_at, so a simple merge keeps the order
	// without re-sorting the whole slice.
	items := mergeByCreatedAtDesc(movies, series)
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.VideoID
	}
	return items, ids, nil
}

func (h *Handler) doListIDs(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]string, error) {
	movieIDs, err := models.ListMovieWatchlistVideoIDs(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	seriesIDs, err := models.ListSeriesWatchlistVideoIDs(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(movieIDs)+len(seriesIDs))
	out = append(out, movieIDs...)
	out = append(out, seriesIDs...)
	return out, nil
}

// doAdd inserts the row, enforcing the free-tier soft cap. Enrichment is
// fire-and-forget on a fresh insert — we don't block the user-visible request
// on a remote TMDB / OMDB lookup, but we do try to populate metadata so the
// next list call has the full card. Existing rows skip enrichment.
func (h *Handler) doAdd(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, ct models.ContentType, source string, limit int) (bool, error) {
	if limit > 0 {
		mc, err := models.CountMovieWatchlist(ctx, db, userID)
		if err != nil {
			return false, err
		}
		sc, err := models.CountSeriesWatchlist(ctx, db, userID)
		if err != nil {
			return false, err
		}
		if mc+sc >= limit {
			return false, errLimitExceeded
		}
	}

	var (
		added bool
		err   error
	)
	switch ct {
	case models.ContentTypeMovie:
		added, err = models.AddToMovieWatchlist(ctx, db, userID, videoID, source)
	case models.ContentTypeSeries:
		added, err = models.AddToSeriesWatchlist(ctx, db, userID, videoID, source)
	}
	if err != nil {
		return false, err
	}
	if added {
		h.tryEnrichMetadata(ctx, db, videoID, ct)
	}
	return added, nil
}

func (h *Handler) doRemove(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, ct models.ContentType) error {
	switch ct {
	case models.ContentTypeMovie:
		return models.RemoveFromMovieWatchlist(ctx, db, userID, videoID)
	case models.ContentTypeSeries:
		return models.RemoveFromSeriesWatchlist(ctx, db, userID, videoID)
	}
	return errors.New("unreachable")
}

// tryEnrichMetadata makes sure movie_metadata / series_metadata has a row
// for this video_id so the JOIN in ListXxxWatchlistItems returns it. We do
// it inline (not async) because:
//
//   - the enrichers are fast on cache hit (TMDB/OMDB own tables) and the
//     usual case is a cache hit because the user just saw the card;
//   - if we miss the lookup the row simply won't appear in the watchlist
//     view, which is worse than an extra ~200ms on add;
//   - failures are logged but don't fail the request — bookmark intent is
//     recorded regardless.
func (h *Handler) tryEnrichMetadata(ctx context.Context, db *pg.DB, videoID string, ct models.ContentType) {
	if h.en == nil || !h.en.HasMappers() {
		return
	}
	md, err := h.en.LookupByVideoID(ctx, videoID, ct)
	if err != nil {
		log.WithError(err).
			WithField("feature", "watchlist").
			WithField("video_id", videoID).
			Warn("metadata lookup failed; row added without metadata")
		return
	}
	if md == nil {
		return
	}
	switch ct {
	case models.ContentTypeMovie:
		if _, err := models.UpsertMovieMetadata(ctx, db, md); err != nil {
			log.WithError(err).
				WithField("feature", "watchlist").
				WithField("video_id", videoID).
				Warn("upsert movie_metadata failed")
		}
	case models.ContentTypeSeries:
		if _, err := models.UpsertSeriesMetadata(ctx, db, md); err != nil {
			log.WithError(err).
				WithField("feature", "watchlist").
				WithField("video_id", videoID).
				Warn("upsert series_metadata failed")
		}
	}
}

// --- helpers ---

func parseContentType(s string) (models.ContentType, error) {
	switch s {
	case "movie":
		return models.ContentTypeMovie, nil
	case "series":
		return models.ContentTypeSeries, nil
	}
	return "", errors.New("type must be 'movie' or 'series'")
}

// limitFor returns the cap for the current user's tier. -1 = unlimited.
// When claims is disabled (no claims-provider configured), GetFromContext
// returns nil and we treat the user as paid — same convention used elsewhere
// in the codebase.
func limitFor(c *gin.Context) int {
	d := claims.GetFromContext(c)
	if d == nil {
		return -1
	}
	if isFreeTier(d) {
		return FreeTierWatchlistLimit
	}
	return -1
}

func isFreeTier(d *proto.GetResponse) bool {
	if d == nil || d.Context == nil || d.Context.Tier == nil {
		return true
	}
	return d.Context.Tier.Id == 0
}

// mergeByCreatedAtDesc merges two created-at-DESC sorted lists into one,
// preserving the order. Movies and series share the WatchlistItem shape so
// the client renders them in a single grid.
func mergeByCreatedAtDesc(a, b []models.WatchlistItem) []models.WatchlistItem {
	out := make([]models.WatchlistItem, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].CreatedAt >= b[j].CreatedAt {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}
