package recommendations

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	ac "github.com/webtor-io/web-ui/services/ai_client"
)

const (
	chipsToolName = "return_chips"

	// Tunables picked so prompts stay comfortably inside Haiku's context.
	// MaxTokens is generous enough for 15 items × ~80 tokens of reason
	// plus JSON boilerplate, but not so big that a runaway response burns
	// our budget. Bumping these requires re-measuring cost.
	aiMaxTokensRecommend = 2048
	aiMaxTokensChips     = 1024
	aiTimeout            = 45 * time.Second

	// Range requested from Claude. We ask for more than we display so the
	// watched-filter and the metadata resolver have buffer to drop items
	// without leaving us with an empty grid. Tuned for end-to-end latency:
	// every extra item Claude has to generate adds ~500ms to the longest
	// pole (output token streaming) and ~one TMDB round-trip to the
	// resolver phase. 6-10 leaves enough headroom for typical drop rates.
	//
	// We do NOT cap the number of items sent to the client — every item
	// the resolver hydrates streams through to the UI, which renders the
	// first AI_RECS_INITIAL_VISIBLE behind a "Show N more" button. The
	// previous server-side cap (maxDisplayItems=6) is intentionally gone
	// so the "Show more" count reflects the real total, not a truncated
	// one.
	minRecItems = 6
	maxRecItems = 10

	desiredChips = 6
)

// AIService is the production implementation of Service. It wires the
// shared AI client, the context builder, the metadata resolver, the quota and
// the distributed chips cache together.
//
// # Caching strategy
//
// Chips are cached in Redis (via ChipsCache) because web-ui runs multiple
// replicas behind a load balancer, and an in-process cache on pod A would
// be invisible to pod B. A free-tier user has exactly one daily request —
// losing it to a cold-cache cross-pod retry would be a product-level bug.
//
// Recommendations are NOT cached on the server at all — every /recommend
// call is unique per (query, history) anyway, and the daily quota is the
// primary rate limiter. Not caching keeps the happy path trivial and
// removes an entire class of double-consume races.
type AIService struct {
	cfg           Config
	client        ac.Client
	freeModel     string
	paidModel     string
	chipsModel    string
	context       *UserContextBuilder
	resolver      *Resolver
	quota         Quota
	chips         ChipsCache
	freshReleases FreshReleasesLoader
}

// modelFor returns the AI model id to use for a given tier. The two
// tiers can be separately configured (Config.FreeModel / Config.PaidModel)
// so paid users can be routed to a more expensive but smarter model
// (e.g. Sonnet) while free users stay on Haiku for cost.
func (s *AIService) modelFor(tier Tier) string {
	if tier == TierPaid {
		return s.paidModel
	}
	return s.freeModel
}

// NewAIService wires all collaborators. Returns nil (not an error) when
// the feature flag is off, the shared client is nil (no API key), or any
// collaborator is missing — handlers treat a nil service as "feature
// disabled" and hide the UI section.
//
// The AI client is constructed by services/ai_client and
// passed in so that the prompt-caching beta header and any future
// transport-level concerns live in exactly one place across the binary.
func NewAIService(
	cfg Config,
	client ac.Client,
	contextBuilder *UserContextBuilder,
	resolver *Resolver,
	quota Quota,
	chips ChipsCache,
	freshReleases FreshReleasesLoader,
) *AIService {
	if !cfg.Enabled {
		log.Info("ai_rec: feature flag off — recommendations service not started")
		return nil
	}
	if client == nil {
		log.Warn("ai_rec: enabled but AI client is nil (no API key) — service disabled")
		return nil
	}
	if contextBuilder == nil || resolver == nil || quota == nil || chips == nil {
		log.Warn("ai_rec: missing collaborators — service disabled")
		return nil
	}

	cfg.Provider = client.Provider()
	freeModel := cfg.ResolveModel(TierFree)
	paidModel := cfg.ResolveModel(TierPaid)
	chipsModel := cfg.ResolveChipsModel()

	s := &AIService{
		cfg:           cfg,
		client:        client,
		freeModel:     freeModel,
		paidModel:     paidModel,
		chipsModel:    chipsModel,
		context:       contextBuilder,
		resolver:      resolver,
		quota:         quota,
		chips:         chips,
		freshReleases: freshReleases,
	}
	log.WithFields(log.Fields{
		"provider":    client.Provider(),
		"free_model":  freeModel,
		"paid_model":  paidModel,
		"chips_model": chipsModel,
	}).Info("ai_rec: AIService ready")
	return s
}

// --- Chips ---

// GenerateChips returns a cached list of chips for the user, or computes a
// fresh one via Claude if cache-missing or ForceRefresh is set.
//
// Chips are cheap — the free tier is allowed to load them on first visit
// without consuming the daily quota. ForceRefresh, however, is what the
// "↻" button in the UI calls, and it *does* consume one quota unit (see
// handlers/discover_ai).
func (s *AIService) GenerateChips(ctx context.Context, req ChipsRequest) (*ChipsResponse, error) {
	uc, err := s.context.Build(ctx, req.UserID, req.Locale, req.Clock)
	if err != nil {
		// Non-fatal: Build still returns a usable UserContext even when the
		// history load failed. Log and keep going so new users (or users
		// whose history lookup transiently errored) still get chips.
		log.WithError(err).WithField("feature", "ai_rec").Warn("user context partial failure")
	}

	// Cold-start optimisation: a user with zero watch history AND empty
	// watchlist gives Claude no personal signal, so calling the model would
	// burn tokens for a generic prompt we can hand-write once. Serve a
	// curated static set instead. We deliberately skip Redis here too — the
	// static path is dirt cheap, and skipping the cache means the moment the
	// user marks their first film as watched (or bookmarks one), the next
	// chip load will go through the real AI path without waiting for a 4h TTL.
	if uc.HistorySize == 0 && uc.WatchlistSize == 0 {
		log.WithField("feature", "ai_rec").
			WithField("locale", uc.Locale).
			WithField("tier", req.Tier.String()).
			Info("cold-start chips served (no AI call)")
		return &ChipsResponse{
			Chips:       defaultChips(uc.Locale),
			GeneratedAt: time.Now().Unix(),
			Tier:        req.Tier.String(),
		}, nil
	}

	key := chipsCacheKey(req.UserID, uc)
	ttl := time.Duration(s.cfg.ChipsTTLSeconds) * time.Second

	if req.ForceRefresh {
		if err := s.chips.Del(ctx, key); err != nil {
			log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache del failed")
		}
	} else {
		if cached, err := s.chips.Get(ctx, key); err != nil {
			log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache get failed")
		} else if cached != nil {
			// Refresh tier tag so UI sees the current subscription state even
			// if the user upgraded mid-TTL.
			cached.Tier = req.Tier.String()
			return cached, nil
		}
	}

	fresh, err := s.generateChipsUncached(ctx, req.Tier, uc)
	if err != nil {
		return nil, err
	}
	if err := s.chips.Set(ctx, key, fresh, ttl); err != nil {
		// Cache write failure is non-fatal — the user still sees their
		// chips, next request just won't hit cache.
		log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache set failed")
	}
	return fresh, nil
}

func (s *AIService) generateChipsUncached(ctx context.Context, tier Tier, uc *UserContext) (*ChipsResponse, error) {
	prompt := userPromptForChips(uc, desiredChips)

	log.WithField("feature", "ai_rec").
		WithField("history_size", uc.HistorySize).
		WithField("day", uc.DayOfWeek).
		WithField("bucket", uc.TimeOfDay).
		Debug("generating chips")

	chips, err := s.callAIForChips(ctx, prompt, tier)
	if err != nil {
		return nil, err
	}
	return &ChipsResponse{
		Chips:       chips,
		GeneratedAt: time.Now().Unix(),
		Tier:        tier.String(),
	}, nil
}

// GenerateChipsStream is the streaming twin of GenerateChips. It pushes one
// StreamEvent of Type "chip" onto `events` as each chip is produced, then a
// terminal "done" or "error". Cold-start defaults and cache hits stream too,
// so the UI's progressive-render path is the same regardless of source.
//
// Quota policy: a normal chips load never consumes a unit (chips are the
// first thing a user sees). ForceRefresh is the only path that costs — and
// in that case the handler is expected to consume the unit before calling us,
// same as the non-streaming GenerateChips contract.
func (s *AIService) GenerateChipsStream(ctx context.Context, req ChipsRequest, events chan<- StreamEvent) {
	defer close(events)

	send := func(t string, data any) bool {
		select {
		case events <- StreamEvent{Type: t, Data: data}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	emitChips := func(chips []Chip) bool {
		for _, c := range chips {
			if !send("chip", c) {
				return false
			}
		}
		return true
	}
	emitDone := func(total int) {
		remaining, rerr := s.quota.Remaining(ctx, req.UserID, req.Tier)
		if rerr != nil {
			log.WithError(rerr).WithField("feature", "ai_rec").Warn("remaining quota lookup failed (chips stream)")
			remaining = -1
		}
		send("done", DoneStreamPayload{
			Total:          total,
			RemainingQuota: remaining,
			DailyQuota:     s.DailyQuota(req.Tier),
			Tier:           req.Tier.String(),
		})
	}
	emitError := func(code string) {
		send("error", ErrorStreamPayload{Code: code, Tier: req.Tier.String()})
	}

	uc, err := s.context.Build(ctx, req.UserID, req.Locale, req.Clock)
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("user context partial failure (chips stream)")
	}

	// Cold-start mirrors GenerateChips: no Claude call, no cache I/O — just
	// stream the curated defaults so the UI shows something instantly.
	// Skipped on ForceRefresh: the user explicitly asked for a fresh set and
	// has already paid one quota unit, so they get a real LLM-generated
	// batch even with empty history.
	if !req.ForceRefresh && uc.HistorySize == 0 && uc.WatchlistSize == 0 {
		log.WithField("feature", "ai_rec").
			WithField("locale", uc.Locale).
			WithField("tier", req.Tier.String()).
			Info("cold-start chips streamed (no AI call)")
		chips := defaultChips(uc.Locale)
		if !emitChips(chips) {
			return
		}
		emitDone(len(chips))
		return
	}

	key := chipsCacheKey(req.UserID, uc)
	ttl := time.Duration(s.cfg.ChipsTTLSeconds) * time.Second

	if req.ForceRefresh {
		if err := s.chips.Del(ctx, key); err != nil {
			log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache del failed (stream)")
		}
	} else {
		if cached, err := s.chips.Get(ctx, key); err != nil {
			log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache get failed (stream)")
		} else if cached != nil {
			if !emitChips(cached.Chips) {
				return
			}
			emitDone(len(cached.Chips))
			return
		}
	}

	// Real LLM path: stream chips off Claude as they arrive, accumulate the
	// full list, then write it back to cache so the next load is instant.
	chipCh := make(chan Chip, desiredChips)
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- s.streamAIChipsText(ctx, userPromptForChipsNDJSON(uc, desiredChips), req.Tier, chipCh)
	}()

	collected := make([]Chip, 0, desiredChips)
	for chip := range chipCh {
		collected = append(collected, chip)
		if !send("chip", chip) {
			// Drain so the producer goroutine doesn't block on a full
			// channel after the client went away.
			for range chipCh {
			}
			return
		}
	}
	if err := <-streamErr; err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("chips stream failed")
		if len(collected) == 0 {
			emitError("upstream_error")
			return
		}
		// Partial result is still useful — fall through to "done" so the UI
		// keeps what it got. Skip cache write so we retry fully next time.
		emitDone(len(collected))
		return
	}
	if len(collected) == 0 {
		emitError("no_chips")
		return
	}

	resp := &ChipsResponse{
		Chips:       collected,
		GeneratedAt: time.Now().Unix(),
		Tier:        req.Tier.String(),
	}
	if err := s.chips.Set(ctx, key, resp, ttl); err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("chips cache set failed (stream)")
	}
	emitDone(len(collected))
}

// --- Quota pass-through ---

// Remaining reports how many quota units the user has left today.
// Non-mutating; safe for GET handlers.
func (s *AIService) Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Remaining(ctx, userID, tier)
}

// ConsumeQuota atomically charges one unit. Returns ErrQuotaExceeded if
// the user is already at their daily cap.
func (s *AIService) ConsumeQuota(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Consume(ctx, userID, tier)
}

// DailyQuota returns the per-day request cap for the given tier — pure
// config lookup, no I/O. The UI uses it to render the remaining counter
// as "N / M" without a second round trip.
func (s *AIService) DailyQuota(tier Tier) int {
	if tier == TierPaid {
		return s.cfg.PaidDailyQuota
	}
	return s.cfg.FreeDailyQuota
}

// QuotaResetAt returns the unix timestamp (seconds) at which the user's
// daily quota next rolls over. Delegates to the underlying Quota
// implementation, which keeps the "midnight UTC vs rolling 24h" decision
// in one place.
func (s *AIService) QuotaResetAt() int64 {
	return s.quota.ResetAt().Unix()
}

// --- Recommend ---

// RecommendStream runs the full pipeline (quota → Claude → resolver) and
// pushes events onto `events` as they happen. Quota is consumed exactly
// once, atomically, before any phase event hits the wire — regardless of
// whether this is a fresh recommend or a refine (distinguished only by
// whether req.History is populated).
//
// Closes `events` before returning. Cancellation: if the upstream ctx is
// cancelled (client disconnect), in-flight goroutines exit and the channel
// closes naturally — no extra wiring needed.
func (s *AIService) RecommendStream(ctx context.Context, req RecommendRequest, events chan<- StreamEvent) {
	defer close(events)

	send := func(ev StreamEvent) bool {
		select {
		case events <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	sendError := func(code string) {
		payload := ErrorStreamPayload{Code: code, Tier: req.Tier.String()}
		// Daily quota / reset / upgrade hint only matter to the UI for
		// the quota-exceeded path; other codes don't need them.
		if code == "quota_exceeded" {
			payload.DailyQuota = s.DailyQuota(req.Tier)
			payload.ResetAt = s.QuotaResetAt()
			// UpgradeQuota tells a free-tier user how much they would
			// get by becoming a supporter. Paid users hitting their
			// own anti-abuse cap don't have an upgrade path, so we
			// leave it at zero (omitempty drops it on the wire).
			if req.Tier == TierFree {
				payload.UpgradeQuota = s.DailyQuota(TierPaid)
			}
		}
		send(StreamEvent{Type: "error", Data: payload})
	}

	q := strings.TrimSpace(req.Query)
	if q == "" {
		sendError("empty_query")
		return
	}
	if s.cfg.MaxQueryLength > 0 && len(q) > s.cfg.MaxQueryLength {
		sendError("query_too_long")
		return
	}

	uc, err := s.context.Build(ctx, req.UserID, req.Locale, req.Clock)
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("user context partial failure")
	}

	isRefine := len(req.History) > 0

	// Quota is consumed up front, before any "phase" event hits the wire.
	// Same semantics as the non-streaming Recommend: one unit per call,
	// regardless of whether this is /recommend or /refine.
	remaining, err := s.quota.Consume(ctx, req.UserID, req.Tier)
	if err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			sendError("quota_exceeded")
		} else {
			log.WithError(err).WithField("feature", "ai_rec").Error("quota consume failed")
			sendError("internal")
		}
		return
	}
	kind := "recommend"
	if isRefine {
		kind = "refine"
	}
	log.WithFields(log.Fields{
		"feature":   "ai_rec",
		"kind":      kind,
		"mode":      "stream",
		"tier":      req.Tier.String(),
		"remaining": remaining,
	}).Info("quota charged")

	// Phase 1: the model is generating recommendations. The UI shows its
	// generation indicator. We stay in this phase until the FIRST
	// resolved item lands on recCh (not when the first delta arrives,
	// because the resolver still needs to do its TMDB hop) — then we
	// flip to "resolving" with the running counter.
	if !send(StreamEvent{Type: "phase", Data: PhaseStreamPayload{Phase: "claude"}}) {
		return
	}

	var prompt string
	if isRefine {
		prompt = userPromptForRefine(uc, q, minRecItems, maxRecItems)
	} else {
		prompt = userPromptForRecommend(uc, q, minRecItems, maxRecItems)
	}

	// End-to-end streaming pipeline:
	//
	//   streamClaudeItems  →  aiItemsCh  →  ResolveStreamFromChannel  →  recCh  →  SSE events
	//
	// The Claude streamer pushes a recommendationItem onto aiItemsCh as soon as
	// the model has finished generating a `{title, year, reason}` triple
	// (typically every ~150-500ms depending on the model). The resolver
	// reads from aiItemsCh and fans out a TMDB lookup for each item
	// concurrently — so item 1's TMDB roundtrip overlaps with Claude
	// generating items 2..N. The first card lands on recCh ~500ms after
	// the first Claude delta, instead of waiting for the whole batch.
	//
	// We deliberately do NOT cancel the Claude stream early. Letting it
	// run to natural completion gives us the message_delta event with
	// final cache_read / cache_write usage counts, which is the only way
	// to know whether prompt caching is actually working. Every item the
	// resolver hydrates streams through to the UI — there's no
	// server-side cap on display count anymore. The frontend renders
	// the first AI_RECS_INITIAL_VISIBLE behind a "Show more" button.
	aiItemsCh := make(chan recommendationItem, aiChannelBuffer)
	streamErrCh := make(chan error, 1)
	go func() {
		// streamAIItemsText (NDJSON / plain text) instead of the
		// tool_use streamClaudeItems — Anthropic buffers tool_use
		// generation server-side, so the latter doesn't actually flow
		// per-token. Plain text streams as it's generated.
		streamErrCh <- s.streamAIItemsText(ctx, uc, prompt, req.History, req.Tier, aiItemsCh)
	}()

	recCh := make(chan Recommendation, r2BufferSize)
	go s.resolver.ResolveStreamFromChannel(ctx, aiItemsCh, models.ContentTypeMovie, req.Locale, recCh)

	sentResolving := false
	sent := 0
	for rec := range recCh {
		// Flip to "resolving" phase on the first card so the UI swaps
		// "Claude думает" → "Подбираю фильмы… (1)". Subsequent items
		// just bump the counter via "item" events.
		if !sentResolving {
			send(StreamEvent{Type: "phase", Data: PhaseStreamPayload{Phase: "resolving"}})
			sentResolving = true
		}

		// Per-item watched + watchlist filter. Two single-row index lookups,
		// ~1-2ms in pg total. We deliberately do this per item rather than
		// batching at the end: the whole point of streaming is "show what
		// you've got". Holding items back to do a batch query would defeat
		// the purpose.
		if s.isAlreadyKnown(ctx, req.UserID, rec.VideoID) {
			continue
		}
		if !send(StreamEvent{Type: "item", Data: rec}) {
			// Client disconnected. We don't return here — we keep
			// pulling from recCh so resolveOne goroutines that are
			// already mid-send don't block on a never-read channel
			// (their own select-on-ctx will then unblock them and
			// they'll exit cleanly). Note: ctx cancellation has
			// already torn down the upstream Claude stream too, so
			// the final message_delta with cache_read/cache_write
			// won't arrive — that's an accepted loss on disconnect.
			continue
		}
		sent++
	}

	// Surface a Claude streaming error ONLY if we never got any items
	// through. If we already showed N cards before things broke, the user
	// would rather see those than a wholesale "something went wrong".
	if err := <-streamErrCh; err != nil && sent == 0 {
		log.WithError(err).WithField("feature", "ai_rec").Error("ai stream failed")
		sendError("claude_failed")
		return
	}

	send(StreamEvent{
		Type: "done",
		Data: DoneStreamPayload{
			Total:          sent,
			RemainingQuota: remaining,
			DailyQuota:     s.DailyQuota(req.Tier),
			Tier:           req.Tier.String(),
		},
	})
}

// aiChannelBuffer is the buffer size for the streamClaudeItems → resolver
// hand-off. Just big enough that Claude doesn't stall on a slow resolver
// goroutine, small enough that cancel propagates quickly.
const aiChannelBuffer = 8

// r2BufferSize sets the buffer for the resolver→stream channel. Just big
// enough to absorb a burst from a freshly-warmed TMDB cache without
// blocking goroutines, but small enough that cancel propagates fast.
const r2BufferSize = 4

// isAlreadyKnown reports whether the user has already engaged with the given
// videoID via either the watched-list or the watchlist. Used by the streaming
// pipeline to drop hallucinated duplicates Claude leaks past the prompt-side
// exclusion. Soft-fails to "unknown" on DB error so a transient blip never
// blocks a recommendation.
func (s *AIService) isAlreadyKnown(ctx context.Context, userID uuid.UUID, videoID string) bool {
	hist := s.context.History()
	watched, err := hist.FilterWatchedVideoIDs(ctx, userID, []string{videoID})
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("watched lookup failed — assuming unwatched")
	} else if len(watched) > 0 {
		return true
	}
	saved, err := hist.FilterWatchlistVideoIDs(ctx, userID, []string{videoID})
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("watchlist lookup failed — assuming not bookmarked")
		return false
	}
	return len(saved) > 0
}

// --- AI generation ---

// buildHistoryMessages converts history into shared AI messages and appends
// the new user prompt as the final turn.
func buildHistoryMessages(history []Message, userPrompt string) []ac.Message {
	messages := make([]ac.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			messages = append(messages, ac.Message{Role: m.Role, Content: m.Content})
		}
	}
	return append(messages, ac.Message{Role: "user", Content: userPrompt})
}

// streamAIItemsText is the streaming AI flow used by RecommendStream.
// It asks the model for plain-text NDJSON output (one self-contained JSON
// object per film, separated by newlines) and parses each object as soon
// as its closing brace lands.
//
// Why NDJSON instead of tool_use: Anthropic buffers tool_use generation
// server-side for many models — the entire JSON tool input arrives as a
// single big chunk, which defeats per-item streaming. Plain text genuinely
// flows token-by-token, so the first card hits the resolver ~500ms after
// the first delta instead of waiting for the whole batch.
//
// Trade-offs:
//   - No JSON-schema validation on Anthropic's side. Claude might add
//     commentary or wrap output in an array. We mitigate with a strict
//     system prompt; the NDJSON scanner ignores anything before the first
//     '{' so an occasional "Sure, here are…" preamble doesn't break it.
//   - Format drift is possible. Each parsed object is still validated by
//     json.Unmarshal into recommendationItem; malformed entries get logged and
//     dropped, the rest survive.
//
// uc is passed for logging only — the prompt is already rendered by the
// caller. HistorySize / WatchlistSize land on the completion log line so
// spend can be attributed to cold-start vs personalised traffic without
// joining two log lines by timestamp.
func (s *AIService) streamAIItemsText(ctx context.Context, uc *UserContext, userPrompt string, history []Message, tier Tier, out chan<- recommendationItem) error {
	defer close(out)

	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()

	// Messages: history (if any) + the new user prompt.
	//
	// We deliberately do NOT use assistant message prefill here. It would
	// be the perfect way to force Claude to start its output with `{`,
	// but Sonnet 4.x explicitly rejects requests where the conversation
	// ends with an assistant turn ("This model does not support
	// assistant message prefill. The conversation must end with a user
	// message."). To stay model-agnostic we rely on the strict system
	// prompt instead, and the NDJSON scanner gracefully ignores any
	// commentary before the first '{' so an occasional "Sure, here are…"
	// preamble doesn't break anything.
	messages := buildHistoryMessages(history, userPrompt)

	// Record streamStart BEFORE NewStreaming so ttft_ms covers the full
	// HTTP round-trip + Claude warmup, not just "time from when we
	// started reading buffered deltas". The SDK's NewStreaming opens the
	// HTTP connection and waits for the first response bytes before
	// returning, so otherwise the metric is misleadingly close to zero.
	streamStart := time.Now()

	// System prompt: two blocks with independent cache breakpoints.
	// Block 1 (base rules, ~2500 tok) is stable across all requests.
	// Block 2 (fresh releases from DB) changes every ~6h when the cron
	// runs, but Anthropic caches each prefix independently, so block 1
	// is always a cache hit even when block 2 refreshes.
	systemBlocks := []ac.SystemBlock{
		{
			Text:  systemPromptNDJSON,
			Cache: true,
		},
	}
	if s.freshReleases != nil {
		if block := s.freshReleases.LoadFreshReleases(ctx); block != "" {
			systemBlocks = append(systemBlocks, ac.SystemBlock{
				Text:  block,
				Cache: true,
			})
		}
	}

	request := ac.Request{
		Model: s.modelFor(tier), MaxTokens: aiMaxTokensRecommend,
		System: systemBlocks, Messages: messages, Temperature: 0.7,
	}

	// The bracket-balance scanner emits each top-level JSON object as
	// soon as the closing brace lands.
	extractor := newNDJSONItemsExtractor(func(raw json.RawMessage) {
		var item recommendationItem
		if err := json.Unmarshal(raw, &item); err != nil {
			log.WithError(err).
				WithField("feature", "ai_rec").
				WithField("raw", string(raw)).
				Warn("ndjson item parse failed")
			return
		}
		select {
		case out <- item:
		case <-ctx.Done():
		}
	})
	usage, deltaCount, err := s.streamText(ctx, request, extractor.write, "recommend", streamStart)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	fields := usageFields(usage)
	fields["provider"] = s.client.Provider()
	fields["feature"] = "ai_rec"
	fields["kind"] = "recommend"
	fields["mode"] = "text"
	fields["history_size"] = uc.HistorySize
	fields["watchlist_size"] = uc.WatchlistSize
	fields["deltas"] = deltaCount
	fields["total_ms"] = time.Since(streamStart).Milliseconds()
	log.WithFields(fields).Info("ai stream complete")

	return nil
}

// callAIForChips runs the chip-generation tool and returns the parsed
// chip list. Chip IDs are derived deterministically from the label so cache
// keys stay stable across regenerations of the same chip text. The tier
// picks which Claude model handles this user.
//
// Caching: prompt caching is intentionally NOT enabled on the chips path.
// systemPrompt is well below Anthropic's caching minimums (1024 tokens for
// Sonnet, 2048 for Haiku) so adding cache_control here would burn a
// cache_write that never gets read. The Redis ChipsCache layer absorbs
// >95% of chip requests anyway. If chips ever go on a hot path again
// (refresh button, periodic regeneration, …), the right fix is to expand
// systemPrompt past the 2048-token threshold AND add CacheControl on the
// system block — see streamAIItemsText for the pattern.
func (s *AIService) callAIForChips(ctx context.Context, userPrompt string, tier Tier) ([]Chip, error) {
	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()

	tool := ac.Tool{
		Name:        chipsToolName,
		Description: "Return a list of suggestion chips tailored to the user and the current moment.",
		Properties: map[string]any{
			"chips": map[string]any{
				"type":     "array",
				"minItems": 4,
				"maxItems": 8,
				"items": map[string]any{
					"type":     "object",
					"required": []string{"label", "query"},
					"properties": map[string]any{
						"label": map[string]any{
							"type":        "string",
							"maxLength":   60,
							"description": "Short user-facing label, in the user's locale.",
						},
						"icon": map[string]any{
							"type":        "string",
							"maxLength":   4,
							"description": "Single emoji that fits the label, or empty string.",
						},
						"query": map[string]any{
							"type":        "string",
							"maxLength":   300,
							"description": "Full-sentence instruction that will be sent to the recommender when this chip is tapped.",
						},
					},
				},
			},
		},
		Required: []string{"chips"},
	}

	resp, err := s.client.CallTool(ctx, ac.Request{
		Model: s.chipsModel, MaxTokens: aiMaxTokensChips,
		System:   []ac.SystemBlock{{Text: systemPrompt}},
		Messages: []ac.Message{{Role: "user", Content: userPrompt}}, Temperature: 0.9,
	}, tool)
	if err != nil {
		return nil, errors.Wrap(err, "AI chips call failed")
	}
	s.logUsage(resp.Usage, "chips")
	input := resp.Input

	var payload struct {
		Chips []struct {
			Label string `json:"label"`
			Icon  string `json:"icon"`
			Query string `json:"query"`
		} `json:"chips"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, errors.Wrap(err, "ai chips: invalid tool input json")
	}

	chips := make([]Chip, 0, len(payload.Chips))
	for _, c := range payload.Chips {
		label := strings.TrimSpace(c.Label)
		query := strings.TrimSpace(c.Query)
		if label == "" || query == "" {
			continue
		}
		chips = append(chips, Chip{
			ID:    shortHash(label),
			Label: label,
			Icon:  strings.TrimSpace(c.Icon),
			Query: query,
		})
	}
	if len(chips) == 0 {
		return nil, ErrNoChips
	}
	return chips, nil
}

// streamAIChipsText is the streaming AI flow for chip generation.
// Mirrors streamAIItemsText (NDJSON plain-text, no tools) but with the
// shorter chips system prompt, chips model, and the smaller token budget.
//
// Why NDJSON instead of the tool_use path used by callAIForChips:
// Anthropic buffers tool_use generation server-side, which defeats per-chip
// streaming entirely. Plain text genuinely flows token-by-token, so the
// first chip lands ~500ms after the first delta instead of waiting for the
// whole batch to materialise.
func (s *AIService) streamAIChipsText(ctx context.Context, userPrompt string, tier Tier, out chan<- Chip) error {
	defer close(out)

	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()

	streamStart := time.Now()

	// System block carries all static rules so its prefix is identical across
	// requests — Anthropic's prompt cache then matches on it and the
	// re-tokenisation work that drove TTFT to ~2.6s drops to ~10% of input
	// tokens, pulling TTFT to ~300-500ms on Haiku within the 5-minute cache
	// window. systemPromptChips is sized just above Haiku's 2048-token cache
	// minimum on purpose; the user message stays short so it doesn't blow the
	// cache key.
	request := ac.Request{
		Model: s.chipsModel, MaxTokens: aiMaxTokensChips,
		System:   []ac.SystemBlock{{Text: systemPromptChips, Cache: true}},
		Messages: []ac.Message{{Role: "user", Content: userPrompt}}, Temperature: 0.9,
	}

	extractor := newNDJSONItemsExtractor(func(raw json.RawMessage) {
		var c struct {
			Label string `json:"label"`
			Icon  string `json:"icon"`
			Query string `json:"query"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			log.WithError(err).
				WithField("feature", "ai_rec").
				WithField("raw", string(raw)).
				Warn("chips ndjson item parse failed")
			return
		}
		label := strings.TrimSpace(c.Label)
		query := strings.TrimSpace(c.Query)
		if label == "" || query == "" {
			return
		}
		chip := Chip{
			ID:    shortHash(label),
			Label: label,
			Icon:  strings.TrimSpace(c.Icon),
			Query: query,
		}
		select {
		case out <- chip:
		case <-ctx.Done():
		}
	})

	usage, deltaCount, err := s.streamText(ctx, request, extractor.write, "chips", streamStart)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	fields := usageFields(usage)
	fields["provider"] = s.client.Provider()
	fields["feature"] = "ai_rec"
	fields["kind"] = "chips"
	fields["mode"] = "text"
	fields["deltas"] = deltaCount
	fields["total_ms"] = time.Since(streamStart).Milliseconds()
	log.WithFields(fields).Info("ai chips stream complete")
	return nil
}

// streamText shares first-token timing while adapters retain final usage.
func (s *AIService) streamText(ctx context.Context, req ac.Request, write func(string), kind string, start time.Time) (ac.Usage, int, error) {
	deltas := 0
	usage, err := s.client.StreamText(ctx, req, func(text string, usage ac.Usage) {
		deltas++
		if deltas == 1 {
			log.WithFields(log.Fields{
				"feature": "ai_rec", "provider": s.client.Provider(), "kind": kind, "mode": "text",
				"ttft_ms":    time.Since(start).Milliseconds(),
				"cache_read": usage.CacheReadTokens, "cache_write": usage.CacheCreateTokens,
			}).Info("ai first delta")
		}
		write(text)
	})
	return usage, deltas, err
}

func usageFields(usage ac.Usage) log.Fields {
	return log.Fields{
		"model": usage.Model, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read": usage.CacheReadTokens, "cache_write": usage.CacheCreateTokens, "stop_reason": usage.StopReason,
	}
}

func (s *AIService) logUsage(usage ac.Usage, kind string) {
	fields := usageFields(usage)
	fields["feature"] = "ai_rec"
	fields["kind"] = kind
	fields["provider"] = s.client.Provider()
	log.WithFields(fields).Info("ai call complete")
}

// --- cache keys ---

// chipsCacheKey produces a stable per-user, per-locale, per-(day, time
// bucket) identifier. Including the day and time bucket in the key means an
// evening user doesn't get chips generated for a morning user — the TTL
// expires naturally as time moves forward, instead of mixing moods.
func chipsCacheKey(userID uuid.UUID, uc *UserContext) string {
	return fmt.Sprintf("%s:%s:%s:%s", userID.String(), uc.Locale, uc.DayOfWeek, uc.TimeOfDay)
}

// shortHash produces a compact hex digest of the given string. Used for
// stable chip IDs, which the frontend uses as React keys.
func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:6])
}
