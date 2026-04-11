// Package discover_ai exposes the AI recommendations backend to the Discover
// Preact app. All routes live under /discover/ai/ and are gated by auth.
//
// HTTP contract
//
//	GET  /discover/ai/chips                  → 200 { chips, generated_at, tier, remaining_quota }
//	POST /discover/ai/chips/refresh          → 200 same shape, consumes 1 quota unit
//	GET  /discover/ai/recommend/stream       → text/event-stream (SSE)
//	GET  /discover/ai/refine/stream          → text/event-stream (SSE), with history
//
// SSE event types: phase, item, done, error.
//
// Status codes
//
//	200 — success (may return empty items)
//	400 — bad JSON / empty query / query too long
//	401 — not logged in (auth middleware handles this)
//	402 — quota exceeded; body exposes tier
//	404 — feature disabled (nil service)
//	500 — upstream failure (Claude / metadata / Redis)
//
// The handler follows the two-level pattern documented in CLAUDE.md: the
// public gin handlers unpack request bodies and map errors to HTTP codes,
// while the business methods take plain values and return typed errors.
package discover_ai

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	csrf "github.com/utrack/gin-csrf"

	proto "github.com/webtor-io/claims-provider/proto"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	rec "github.com/webtor-io/web-ui/services/recommendations"
)

// Handler wraps the recommendations Service with a gin-facing layer.
type Handler struct {
	svc rec.Service
}

// RegisterHandler mounts all /discover/ai/* routes on the given engine.
// `svc` MUST be non-nil — call sites are expected to skip registration
// entirely when the feature is disabled (see serve.go: rec.New returns
// nil and the caller bails out before reaching here). When the routes
// don't exist, gin's default 404 is enough for the frontend to hide the
// section.
//
// Recommend / refine are GET (not POST) so the browser's native EventSource
// works without a custom fetch+ReadableStream parser; this mirrors the
// pattern in handlers/resource/status.go and lib/progressLog.js. The request
// payload (query, locale, clock, history) moves into the query string,
// capped on the client to keep URL length comfortably under proxy limits.
func RegisterHandler(r *gin.Engine, svc rec.Service) {
	h := &Handler{svc: svc}
	gr := r.Group("/discover/ai")
	gr.Use(auth.HasAuth)
	gr.GET("/chips", h.getChips)
	gr.POST("/chips/refresh", h.refreshChips)
	gr.GET("/recommend/stream", h.recommendStream)
	gr.GET("/refine/stream", h.refineStream)
}

// --- request / response dtos ---

// clockDTO matches the JSON and query-string shape the Preact client sends
// for its local wall clock. See assets/src/js/lib/discover/aiClient.js.
type clockDTO struct {
	DayOfWeek string `form:"day" json:"day"`
	Hour      int    `form:"hour" json:"hour"`
}

func (d clockDTO) toClock() rec.ClientClock {
	return rec.ClientClock{DayOfWeek: d.DayOfWeek, Hour: d.Hour}
}

type messageDTO struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m messageDTO) toMessage() rec.Message {
	return rec.Message{Role: m.Role, Content: m.Content}
}

type chipsRefreshRequestDTO struct {
	Locale string   `json:"locale"`
	Clock  clockDTO `json:"clock"`
}

// errorBody is the JSON body returned on 4xx errors. "code" is a short
// machine-readable token the frontend can branch on without parsing
// English error messages.
//
// DailyQuota, ResetAt and UpgradeQuota are populated on quota_exceeded so
// the UI can render the full upgrade hint ("0 / 1, resets in 10h 30m,
// upgrade for 100/day") without a separate lookup. UpgradeQuota is only
// set for free users — paid hitting their own anti-abuse cap have no
// upgrade path.
type errorBody struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Tier         string `json:"tier,omitempty"`
	DailyQuota   int    `json:"daily_quota,omitempty"`
	UpgradeQuota int    `json:"upgrade_quota,omitempty"`
	ResetAt      int64  `json:"reset_at,omitempty"`
}

// chipsResponseBody is the JSON shape returned by both GET /chips and
// POST /chips/refresh so the frontend can use a single parser.
// DailyQuota is the per-day cap for the user's current tier — sent
// alongside RemainingQuota so the UI can render "N / M left" without a
// second round trip.
type chipsResponseBody struct {
	Chips          []rec.Chip `json:"chips"`
	GeneratedAt    int64      `json:"generated_at"`
	Tier           string     `json:"tier"`
	RemainingQuota int        `json:"remaining_quota"`
	DailyQuota     int        `json:"daily_quota"`
}

// --- Level 1: gin handlers ---

func (h *Handler) getChips(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	tier := tierFromClaims(claims.GetFromContext(c))
	clock, locale := readClockLocaleFromQuery(c)

	resp, err := h.svc.GenerateChips(c.Request.Context(), rec.ChipsRequest{
		UserID: user.ID,
		Tier:   tier,
		Locale: locale,
		Clock:  clock,
	})
	if err != nil {
		h.writeServiceError(c, err, tier)
		return
	}

	remaining, rerr := h.svc.Remaining(c.Request.Context(), user.ID, tier)
	if rerr != nil {
		log.WithError(rerr).WithField("feature", "ai_rec").Warn("remaining quota lookup failed")
		remaining = -1
	}
	c.JSON(http.StatusOK, chipsResponseBody{
		Chips:          resp.Chips,
		GeneratedAt:    resp.GeneratedAt,
		Tier:           resp.Tier,
		RemainingQuota: remaining,
		DailyQuota:     h.svc.DailyQuota(tier),
	})
}

func (h *Handler) refreshChips(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	tier := tierFromClaims(claims.GetFromContext(c))

	// Prefer a JSON body; fall back to query params so curl / health probes
	// still work from the command line. We intentionally swallow the bind
	// error: an empty body / wrong content-type just means "use the query
	// param fallback below", not "fail the request".
	var body chipsRefreshRequestDTO
	_ = c.ShouldBindJSON(&body)
	clock := body.Clock.toClock()
	locale := body.Locale
	if !clock.IsValid() {
		clock, locale = readClockLocaleFromQuery(c)
	}

	// Force refresh is an explicit user action and costs one unit.
	remaining, err := h.svc.ConsumeQuota(c.Request.Context(), user.ID, tier)
	if err != nil {
		h.writeServiceError(c, err, tier)
		return
	}

	resp, err := h.svc.GenerateChips(c.Request.Context(), rec.ChipsRequest{
		UserID:       user.ID,
		Tier:         tier,
		Locale:       locale,
		Clock:        clock,
		ForceRefresh: true,
	})
	if err != nil {
		h.writeServiceError(c, err, tier)
		return
	}
	c.JSON(http.StatusOK, chipsResponseBody{
		Chips:          resp.Chips,
		GeneratedAt:    resp.GeneratedAt,
		Tier:           resp.Tier,
		RemainingQuota: remaining,
		DailyQuota:     h.svc.DailyQuota(tier),
	})
}

// recommendStream / refineStream emit text/event-stream frames as the
// recommender works, so the browser's native EventSource can render cards
// as soon as the resolver finishes each one — instead of waiting 20-30s
// for the whole batch. Quota is consumed exactly once, atomically, before
// the first event hits the wire.
//
// Wire details follow the project's existing pattern (see
// handlers/resource/status.go and lib/progressLog.js):
//
//   - GET request so EventSource works without a custom POST/fetch parser.
//   - Payload (query, locale, clock, history) lives in the query string;
//     the client trims history to the last few turns to keep the URL
//     comfortably under proxy length limits.
//   - CSRF passed via _csrf query param because EventSource cannot set
//     custom headers.
//   - gin's c.Stream + c.SSEvent handle the loop, flushing, named events
//     and client-disconnect detection.
func (h *Handler) recommendStream(c *gin.Context) {
	h.handleRecommendStream(c, false)
}

func (h *Handler) refineStream(c *gin.Context) {
	h.handleRecommendStream(c, true)
}

func (h *Handler) handleRecommendStream(c *gin.Context, withHistory bool) {
	// CSRF check via query param (EventSource is GET-only and cannot set
	// custom headers — see resource/status.go for the same trick).
	if token := c.Query("_csrf"); token == "" || token != csrf.GetToken(c) {
		c.String(http.StatusForbidden, "CSRF token mismatch")
		return
	}

	user := auth.GetUserFromContext(c)
	tier := tierFromClaims(claims.GetFromContext(c))

	req, parseErr := parseStreamRequest(c, withHistory)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, errorBody{Code: "bad_request", Message: parseErr.Error()})
		return
	}
	req.UserID = user.ID
	req.Tier = tier

	// SSE headers MUST be set before c.Stream — gin's c.Stream does NOT
	// auto-emit them. Without an explicit Content-Type: text/event-stream
	// any HTTP proxy in the path (including webpack-dev-server in local
	// dev) treats the response as a regular completion-on-EOF body and
	// buffers it, defeating real per-event streaming. `no-transform` in
	// Cache-Control is the formal "do not buffer or modify" hint for
	// well-behaved intermediates. Mirrors handlers/job/handler.go and
	// handlers/resource/status.go.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Access-Control-Allow-Origin", "*")

	// Spawn the service into a goroutine; we read events off the channel
	// from the gin Stream callback and translate them into SSE events.
	events := make(chan rec.StreamEvent, 8)
	go h.svc.RecommendStream(c.Request.Context(), req, events)

	c.Stream(func(_ io.Writer) bool {
		select {
		case <-c.Request.Context().Done():
			return false
		case ev, ok := <-events:
			if !ok {
				return false
			}
			c.SSEvent(ev.Type, ev.Data)
			// Terminal events: close the connection. Gin will flush the
			// final payload before tearing down the loop.
			return ev.Type != "done" && ev.Type != "error"
		}
	})
}

// parseStreamRequest extracts a RecommendRequest from query parameters.
// History (if any) is JSON-encoded in the `history` query param to keep
// nested structure intact without inventing a custom encoding.
func parseStreamRequest(c *gin.Context, withHistory bool) (rec.RecommendRequest, error) {
	clock, locale := readClockLocaleFromQuery(c)
	req := rec.RecommendRequest{
		Query:  c.Query("query"),
		Locale: locale,
		Clock:  clock,
	}
	if withHistory {
		raw := c.Query("history")
		if raw != "" {
			var msgs []messageDTO
			if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
				return req, errors.Wrap(err, "invalid history JSON")
			}
			req.History = make([]rec.Message, 0, len(msgs))
			for _, m := range msgs {
				req.History = append(req.History, m.toMessage())
			}
		}
	}
	return req, nil
}

// --- helpers ---

// readClockLocaleFromQuery extracts the browser's local clock from query
// parameters on a GET request. `day` is the English weekday name as
// produced by `Intl.DateTimeFormat("en-US", {weekday: "long"})`, `hour`
// is the local hour 0..23, `locale` is the two-letter locale prefix.
//
// On a malformed `hour` (non-numeric, missing) we return Hour=-1 — that
// fails ClientClock.IsValid() and the context builder falls back to the
// neutral "Saturday afternoon" default. We deliberately do NOT silently
// coerce to 0 because it would push every garbage request into the
// "night" bucket.
func readClockLocaleFromQuery(c *gin.Context) (rec.ClientClock, string) {
	day := c.Query("day")
	hour := -1
	if raw := c.Query("hour"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			hour = v
		}
	}
	return rec.ClientClock{DayOfWeek: day, Hour: hour}, c.Query("locale")
}

// tierFromClaims maps the richer claims-provider tier into the two-value
// Tier enum the recommendations service uses. Tier.Id == 0 is the free
// tier in the claims protocol (see services/claims/claims.go:163).
func tierFromClaims(d *proto.GetResponse) rec.Tier {
	if d == nil || d.Context == nil || d.Context.Tier == nil {
		return rec.TierFree
	}
	if d.Context.Tier.Id == 0 {
		return rec.TierFree
	}
	return rec.TierPaid
}

// writeServiceError maps service-layer sentinel errors onto HTTP codes.
// Everything unknown becomes 500 with the error logged.
//
// It's a method (not a free function) so it can reach h.svc.DailyQuota
// when populating quota / chips response bodies — the UI needs the
// daily cap to render "N / M".
func (h *Handler) writeServiceError(c *gin.Context, err error, tier rec.Tier) {
	switch {
	case errors.Is(err, rec.ErrQuotaExceeded):
		body := errorBody{
			Code:       "quota_exceeded",
			Message:    "daily AI recommendations quota exceeded",
			Tier:       tier.String(),
			DailyQuota: h.svc.DailyQuota(tier),
			ResetAt:    h.svc.QuotaResetAt(),
		}
		// Only free users have an upgrade path; paid users hitting
		// the anti-abuse cap can't escape it by spending more.
		if tier == rec.TierFree {
			body.UpgradeQuota = h.svc.DailyQuota(rec.TierPaid)
		}
		c.JSON(http.StatusPaymentRequired, body)
	case errors.Is(err, rec.ErrEmptyQuery):
		c.JSON(http.StatusBadRequest, errorBody{Code: "empty_query", Message: "query is required"})
	case errors.Is(err, rec.ErrQueryTooLong):
		c.JSON(http.StatusBadRequest, errorBody{Code: "query_too_long", Message: "query is too long"})
	case errors.Is(err, rec.ErrNoChips):
		// Chips generation produced nothing usable but the call itself
		// succeeded — surface 200 with an empty chip list so the UI can
		// show its empty state instead of an error toast.
		c.JSON(http.StatusOK, chipsResponseBody{
			Tier:       tier.String(),
			DailyQuota: h.svc.DailyQuota(tier),
		})
	default:
		log.WithError(err).WithField("feature", "ai_rec").Error("service call failed")
		c.JSON(http.StatusInternalServerError, errorBody{Code: "internal", Message: "something went wrong"})
	}
}
