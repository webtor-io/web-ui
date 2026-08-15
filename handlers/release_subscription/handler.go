// Package release_subscription exposes content subscriptions over HTTP.
//
// Two surfaces speak to the same business logic, because two kinds of page
// create subscriptions:
//
//	JSON, for the Discover Preact app
//	  GET    /discover/subscriptions        → 200 { items, limit, count }
//	  GET    /discover/subscriptions/ids    → 200 { items, limit, count }
//	  POST   /discover/subscriptions        → 200 { added } / 402 limit / 409 not eligible
//	  DELETE /discover/subscriptions/:kind/:video_id[?season=N]
//
//	Forms, for the server-rendered banner and the profile list
//	  POST /subscription/add
//	  POST /subscription/delete/:id
//	  POST /subscription/update
//
// Two-level handler pattern (per CLAUDE.md): the gin handlers below unpack
// requests and map errors onto status codes; everything that decides
// anything lives in services/release_subscription.
package release_subscription

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"

	hc "github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	rs "github.com/webtor-io/web-ui/services/release_subscription"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	pg  *cs.PG
	svc *rs.Service
	tb  template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, svc *rs.Service) {
	h := &Handler{
		pg:  pg,
		svc: svc,
		tb:  tm.MustRegisterViews("subscription/*").WithLayout("main"),
	}

	jr := r.Group("/discover/subscriptions")
	jr.Use(auth.HasAuth)
	jr.GET("", h.list)
	jr.GET("/ids", h.list)
	jr.POST("", h.add)
	jr.DELETE("/:kind/:video_id", h.remove)

	fr := r.Group("/subscription")
	fr.Use(auth.HasAuth)
	fr.POST("/add", h.formAdd)
	fr.POST("/delete/:id", h.formDelete)
	fr.POST("/update", h.formUpdate)

	// Deliberately outside the auth group: this is the link in an email,
	// and the signed token is what authorizes it. Requiring a session would
	// mean the reader has to log in to stop receiving mail.
	r.GET("/subscription/unsubscribe/:token", h.unsubscribeByToken)
}

// unsubscribedData drives the confirmation page.
type unsubscribedData struct {
	Title  string
	Failed bool
}

// --- DTOs ---

type item struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	VideoID   string `json:"video_id"`
	Season    int    `json:"season,omitempty"`
	Title     string `json:"title,omitempty"`
	PosterURL string `json:"poster_url,omitempty"`
	State     string `json:"state"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

type listResponse struct {
	Items []item `json:"items"`
	Count int    `json:"count"`
	Limit int    `json:"limit"` // -1 = unlimited (paid)
}

type addRequest struct {
	Kind    string `json:"kind"`
	VideoID string `json:"video_id"`
	Season  int    `json:"season"`
	Source  string `json:"source"`
}

type errorBody struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Limit   int    `json:"limit,omitempty"`
}

type successBody struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Added   bool   `json:"added,omitempty"`
	Removed bool   `json:"removed,omitempty"`
	Item    *item  `json:"item,omitempty"`
}

// --- Level 1: JSON handlers ---

func (h *Handler) list(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	subs, err := h.svc.List(c.Request.Context(), user.ID)
	if err != nil {
		log.WithError(err).WithField("feature", "release_subscription").Error("list failed")
		writeInternal(c)
		return
	}
	items := make([]item, 0, len(subs))
	for i := range subs {
		items = append(items, toItem(&subs[i]))
	}
	c.JSON(http.StatusOK, listResponse{Items: items, Count: len(items), Limit: limitFor(c)})
}

func (h *Handler) add(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	var req addRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody{
			Status: "error", Code: "bad_request",
			Message: i18n.T(c, "discover.subscriptions.addFailed"),
		})
		return
	}

	sub, added, err := h.svc.Subscribe(c.Request.Context(), user, rs.Request{
		Kind:    req.Kind,
		VideoID: req.VideoID,
		Season:  req.Season,
		Source:  req.Source,
		Lang:    i18n.GetLang(c),
	}, limitFor(c))
	if err != nil {
		h.writeSubscribeError(c, err)
		return
	}

	body := successBody{Status: "success", Added: added}
	if added {
		body.Message = i18n.T(c, "discover.subscriptions.added")
	}
	if sub != nil {
		it := toItem(sub)
		body.Item = &it
	}
	c.JSON(http.StatusOK, body)
}

func (h *Handler) remove(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	season, err := parseSeasonQuery(c.Query("season"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorBody{
			Status: "error", Code: "bad_season",
			Message: i18n.T(c, "discover.subscriptions.removeFailed"),
		})
		return
	}

	removed, err := h.svc.Unsubscribe(c.Request.Context(), user, rs.Request{
		Kind:    c.Param("kind"),
		VideoID: c.Param("video_id"),
		Season:  season,
	})
	if err != nil {
		if errors.Is(err, rs.ErrBadRequest) {
			c.JSON(http.StatusBadRequest, errorBody{
				Status: "error", Code: "bad_request",
				Message: i18n.T(c, "discover.subscriptions.removeFailed"),
			})
			return
		}
		log.WithError(err).WithField("feature", "release_subscription").Error("remove failed")
		writeInternal(c)
		return
	}
	c.JSON(http.StatusOK, successBody{
		Status:  "success",
		Message: i18n.T(c, "discover.subscriptions.removed"),
		Removed: removed,
	})
}

// writeSubscribeError maps the service's typed errors onto the three answers
// the client can act on: fix the request, upgrade, or nothing (this content
// has no future releases to wait for).
func (h *Handler) writeSubscribeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, rs.ErrLimitExceeded):
		c.JSON(http.StatusPaymentRequired, errorBody{
			Status: "error", Code: "limit_exceeded",
			Message: i18n.T(c, "discover.subscriptions.limitReached"),
			Limit:   limitFor(c),
		})
	case errors.Is(err, rs.ErrNotEligible):
		c.JSON(http.StatusConflict, errorBody{
			Status: "error", Code: "not_eligible",
			Message: i18n.T(c, "discover.subscriptions.notEligible"),
		})
	case errors.Is(err, rs.ErrBadRequest):
		c.JSON(http.StatusBadRequest, errorBody{
			Status: "error", Code: "bad_request",
			Message: i18n.T(c, "discover.subscriptions.addFailed"),
		})
	default:
		log.WithError(err).WithField("feature", "release_subscription").Error("add failed")
		writeInternal(c)
	}
}

func writeInternal(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, errorBody{
		Status:  "error",
		Code:    "internal",
		Message: i18n.T(c, "discover.somethingWrong"),
	})
}

// --- Level 1: form handlers ---

func (h *Handler) formAdd(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	season, err := parseSeasonQuery(c.PostForm("season"))
	if err != nil {
		web.RedirectWithError(c, web.NewUserError("error.subscriptionFailed", err))
		return
	}
	_, _, err = h.svc.Subscribe(c.Request.Context(), user, rs.Request{
		Kind:    c.PostForm("kind"),
		VideoID: c.PostForm("video_id"),
		Season:  season,
		Source:  c.PostForm("source"),
		Lang:    i18n.GetLang(c),
	}, limitFor(c))
	if err != nil {
		log.WithError(err).WithField("feature", "release_subscription").Error("form add failed")
		web.RedirectWithError(c, userError(err))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.subscriptionAdded")
}

func (h *Handler) formDelete(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		web.RedirectWithError(c, web.NewUserError("error.subscriptionFailed", err))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), user, id); err != nil {
		log.WithError(err).WithField("feature", "release_subscription").Error("form delete failed")
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.subscriptionRemoved")
}

// formUpdate applies the profile list form: staged deletions plus the
// per-row enable toggle. There is no order to apply — subscriptions have no
// priority between them — so of the shared list-form fields only two are
// read.
func (h *Handler) formUpdate(c *gin.Context) {
	deleted := c.PostForm("deleted_subscriptions")
	isEnabled := func(id uuid.UUID) bool {
		return c.PostForm("subscription_"+id.String()+"_enabled") == "on"
	}
	if err := h.updateSubscriptions(c, deleted, isEnabled); err != nil {
		log.WithError(err).WithField("feature", "release_subscription").Error("form update failed")
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
}

// unsubscribeByToken serves the one-click link. It answers with a page
// either way: a token that no longer resolves means the subscription is
// already gone, which is the outcome the reader wanted.
func (h *Handler) unsubscribeByToken(c *gin.Context) {
	sub, err := h.svc.DeleteByToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		log.WithError(err).WithField("feature", "release_subscription").Warn("unsubscribe link rejected")
		h.tb.Build("subscription/unsubscribed").HTML(http.StatusOK, web.NewContext(c).WithData(&unsubscribedData{Failed: true}))
		return
	}
	data := &unsubscribedData{}
	if sub != nil {
		data.Title = sub.GetTitle()
	}
	h.tb.Build("subscription/unsubscribed").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

// --- Level 2 ---

// updateSubscriptions applies deletions first, then writes only the rows
// whose toggle actually moved. isEnabled is passed in so this half stays
// free of gin.Context.
func (h *Handler) updateSubscriptions(c *gin.Context, deletedStr string, isEnabled func(uuid.UUID) bool) error {
	ctx := c.Request.Context()
	user := auth.GetUserFromContext(c)

	for _, raw := range hc.SplitIDs(deletedStr) {
		id, err := uuid.FromString(raw)
		if err != nil {
			continue
		}
		if err := h.svc.Delete(ctx, user, id); err != nil {
			return err
		}
	}

	subs, err := h.svc.List(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		want := isEnabled(sub.ID)
		if want == sub.Enabled {
			continue
		}
		if err := h.svc.SetEnabled(ctx, user.ID, sub.ID, want); err != nil {
			return err
		}
	}
	return nil
}

// --- helpers ---

func toItem(s *models.ReleaseSubscription) item {
	it := item{
		ID:        s.ID.String(),
		Kind:      s.Kind,
		VideoID:   s.VideoID,
		Season:    s.GetSeason(),
		State:     s.State,
		Enabled:   s.Enabled,
		CreatedAt: s.CreatedAt.Unix(),
	}
	if s.Title != nil {
		it.Title = *s.Title
	}
	if s.PosterURL != nil {
		it.PosterURL = *s.PosterURL
	}
	return it
}

// parseSeasonQuery reads the optional season parameter. An absent value is
// not an error — that is what a movie subscription looks like.
func parseSeasonQuery(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// userError turns the service's typed errors into messages the form pages
// can render. Anything else stays as-is and surfaces as the generic failure.
func userError(err error) error {
	switch {
	case errors.Is(err, rs.ErrLimitExceeded):
		return web.NewUserError("error.subscriptionLimit", err)
	case errors.Is(err, rs.ErrNotEligible):
		return web.NewUserError("error.subscriptionNotEligible", err)
	case errors.Is(err, rs.ErrBadRequest):
		return web.NewUserError("error.subscriptionFailed", err)
	}
	return err
}

// limitFor returns the cap for the current user's tier. -1 = unlimited.
// No claims at all (claims-provider disabled) means paid, the convention the
// rest of the codebase follows.
func limitFor(c *gin.Context) int {
	d := claims.GetFromContext(c)
	if d == nil {
		return -1
	}
	free := d.Context == nil || d.Context.Tier == nil || d.Context.Tier.Id == 0
	return rs.Limit(free)
}
