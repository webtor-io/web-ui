package torznab_indexer

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/torznab"
	"github.com/webtor-io/web-ui/services/web"
)

// freeTierLimit matches the Stremio addon limit — both are user-configured
// upstreams and there is no reason for a user to reason about two different
// allowances.
const freeTierLimit = 3

// validateTimeout bounds the caps probe that runs while the user waits on
// the add form.
const validateTimeout = 15 * time.Second

type Handler struct {
	pg        *cs.PG
	validator *torznab.Validator
	domain    string
}

func RegisterHandler(c *cli.Context, v *torznab.Validator, r *gin.Engine, pg *cs.PG) error {
	d := c.String(common.DomainFlag)
	if d != "" {
		u, err := url.Parse(d)
		if err != nil {
			return err
		}
		d = u.Hostname()
	}

	h := &Handler{
		pg:        pg,
		validator: v,
		domain:    d,
	}

	gr := r.Group("/torznab/indexer")
	gr.Use(auth.HasAuth)
	gr.POST("/add", h.add)
	gr.POST("/delete/:id", h.delete)
	gr.POST("/update", h.update)
	gr.POST("/:id/refresh-caps", h.refreshCaps)
	return nil
}

func (s *Handler) add(c *gin.Context) {
	rawURL := strings.TrimSpace(c.PostForm("url"))
	apiKey := strings.TrimSpace(c.PostForm("api_key"))
	user := auth.GetUserFromContext(c)
	cla := claims.GetFromContext(c)
	if err := s.addIndexer(c.Request.Context(), rawURL, apiKey, user, cla); err != nil {
		log.WithError(err).Error("failed to add torznab indexer")
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.indexerAdded")
}

func (s *Handler) addIndexer(ctx context.Context, rawURL, apiKey string, user *auth.User, cla *claims.Data) error {
	feedURL, key, err := torznab.ParseFeedURL(rawURL, apiKey)
	if err != nil {
		return err
	}

	parsed, err := url.Parse(feedURL)
	if err != nil {
		return errors.New("invalid URL format")
	}
	if s.domain != "" && parsed.Hostname() == s.domain {
		return errors.New("cannot add Webtor's own URL as an indexer")
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	freeTier := cla.Context.Tier.Id == 0
	if freeTier {
		if err := s.checkFreeTierLimit(ctx, user); err != nil {
			return err
		}
	}

	exists, err := models.TorznabIndexerExists(ctx, db, user.ID, feedURL)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("indexer already exists")
	}

	// Unlike a Stremio addon, an indexer that fails its probe is not saved:
	// the two failure modes users actually hit — a wrong API key and an
	// address our servers cannot route to — are both permanent, and storing
	// the row would turn them into a silently empty source instead of an
	// error on the form.
	vCtx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()
	name, caps, err := s.validator.ValidateAndFetch(vCtx, torznab.Endpoint{URL: feedURL, APIKey: key})
	if err != nil {
		// "We cannot route to this address" and "your key is wrong" are the
		// two mistakes users actually make, and only the second one can be
		// fixed on this form — so they must not share the generic message.
		if torznab.IsUnreachable(err) {
			return web.NewUserError("error.indexerUnreachable", err)
		}
		return err
	}

	// Re-check after the probe. The check above answers the user fast, but
	// the probe in between takes up to 15 seconds — wide enough that
	// concurrent adds all read the same count and all insert.
	if freeTier {
		if err := s.checkFreeTierLimit(ctx, user); err != nil {
			return err
		}
	}

	return models.CreateTorznabIndexer(ctx, db, user.ID, feedURL, key, name, caps)
}

func (s *Handler) checkFreeTierLimit(ctx context.Context, user *auth.User) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	currentCount, err := models.CountUserTorznabIndexers(ctx, db, user.ID)
	if err != nil {
		return err
	}
	if currentCount >= freeTierLimit {
		return errors.New("maximum 3 indexers allowed for free tier")
	}
	return nil
}

func (s *Handler) delete(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if err := s.deleteIndexer(c.Request.Context(), c.Param("id"), user); err != nil {
		log.WithError(err).Error("failed to delete torznab indexer")
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.indexerDeleted")
}

func (s *Handler) deleteIndexer(ctx context.Context, idStr string, user *auth.User) error {
	id, err := uuid.FromString(idStr)
	if err != nil {
		return err
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.DeleteUserTorznabIndexer(ctx, db, id, user.ID)
}

func (s *Handler) update(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	deleted := c.PostForm("deleted_indexers")
	order := c.PostForm("indexer_order")
	enabled := func(id uuid.UUID) bool {
		return c.PostForm("indexer_"+id.String()+"_enabled") == "on"
	}
	if err := s.updateIndexers(c.Request.Context(), deleted, order, enabled, user); err != nil {
		log.WithError(err).Error("failed to update torznab indexers")
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
}

// updateIndexers applies the three things the list form can change:
// deletions, per-indexer enable state and priority order. isEnabled is passed
// in so this stays free of gin.Context.
func (s *Handler) updateIndexers(ctx context.Context, deletedStr, orderStr string, isEnabled func(uuid.UUID) bool, user *auth.User) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no database connection available")
	}

	for _, idStr := range splitIDs(deletedStr) {
		if err := s.deleteIndexer(ctx, idStr, user); err != nil {
			log.WithError(err).
				WithField("user_id", user.ID).
				WithField("indexer_id", idStr).
				Error("failed to delete torznab indexer")
		}
	}

	indexers, err := models.GetAllUserTorznabIndexers(ctx, db, user.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get user indexers")
	}

	order := splitIDs(orderStr)
	for i := range indexers {
		indexer := &indexers[i]
		changed := false

		if enabled := isEnabled(indexer.ID); indexer.Enabled != enabled {
			indexer.Enabled = enabled
			changed = true
		}
		if pos := indexOf(order, indexer.ID.String()); pos >= 0 {
			// Higher index in the submitted order = lower priority, same
			// convention as the addon list.
			priority := int16(len(order) - pos)
			if indexer.Priority != priority {
				indexer.Priority = priority
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := models.UpdateTorznabIndexer(ctx, db, indexer); err != nil {
			return errors.Wrap(err, "failed to update indexer")
		}
	}

	log.WithField("user_id", user.ID).Info("torznab indexers updated successfully")
	return nil
}

// refreshCapsResponse is what the profile UI needs to redraw a row without a
// full page reload.
type refreshCapsResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// refreshCaps re-probes an indexer. Capabilities change when a user adds
// trackers to their Jackett, and the query shape we pick depends on them.
func (s *Handler) refreshCaps(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	indexerID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid indexer id"})
		return
	}

	db := s.pg.Get()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no db"})
		return
	}

	indexer, err := models.GetTorznabIndexerByID(c.Request.Context(), db, indexerID)
	if err != nil {
		log.WithError(err).Error("failed to load indexer for refresh-caps")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if indexer == nil || indexer.UserID != user.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "indexer not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), validateTimeout)
	defer cancel()
	name, caps, err := s.validator.ValidateAndFetch(ctx, torznab.Endpoint{
		URL:    indexer.Url,
		APIKey: indexer.GetApiKey(),
	})
	if err != nil {
		log.WithError(err).WithField("indexer_id", indexerID).Info("refresh-caps: indexer probe failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if err := models.UpdateTorznabIndexerCaps(c.Request.Context(), db, indexerID, user.ID, name, caps); err != nil {
		log.WithError(err).Error("refresh-caps: db update failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	// The row shows the derived label, not the bare server title — see
	// models.TorznabIndexer.GetName.
	display := (&models.TorznabIndexer{Name: &name, Url: indexer.Url}).GetName()
	c.JSON(http.StatusOK, refreshCapsResponse{
		ID:        indexerID.String(),
		Name:      display,
		FetchedAt: time.Now(),
	})
}

func splitIDs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func indexOf(ids []string, want string) int {
	for i, id := range ids {
		if id == want {
			return i
		}
	}
	return -1
}
