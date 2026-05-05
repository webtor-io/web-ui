package recommendations

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
)

const (
	chipsToolName = "return_chips"

	// Tunables picked so prompts stay comfortably inside Haiku's context.
	// MaxTokens is generous enough for 15 items × ~80 tokens of reason
	// plus JSON boilerplate, but not so big that a runaway response burns
	// our budget. Bumping these requires re-measuring cost.
	claudeMaxTokensRecommend = 2048
	claudeMaxTokensChips     = 1024
	claudeTimeout            = 45 * time.Second

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

// ClaudeService is the production implementation of Service. It wires the
// Anthropic SDK, the context builder, the metadata resolver, the quota and
// the distributed chips cache together.
//
// Caching strategy
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
type ClaudeService struct {
	cfg            Config
	client         *anthropic.Client
	freeModel      anthropic.Model
	paidModel      anthropic.Model
	chipsModel     anthropic.Model
	context        *UserContextBuilder
	resolver       *Resolver
	quota          Quota
	chips          ChipsCache
	freshReleases  FreshReleasesLoader
}

// modelFor returns the Claude model id to use for a given tier. The two
// tiers can be separately configured (Config.FreeModel / Config.PaidModel)
// so paid users can be routed to a more expensive but smarter model
// (e.g. Sonnet) while free users stay on Haiku for cost.
func (s *ClaudeService) modelFor(tier Tier) anthropic.Model {
	if tier == TierPaid {
		return s.paidModel
	}
	return s.freeModel
}

// NewClaudeService wires all collaborators. Returns nil (not an error) when
// the feature flag is off or the Anthropic API key is missing — handlers
// treat a nil service as "feature disabled" and hide the UI section.
func NewClaudeService(
	cfg Config,
	contextBuilder *UserContextBuilder,
	resolver *Resolver,
	quota Quota,
	chips ChipsCache,
	freshReleases FreshReleasesLoader,
) *ClaudeService {
	if !cfg.Enabled {
		log.Info("ai_rec: feature flag off — recommendations service not started")
		return nil
	}
	if strings.TrimSpace(cfg.AnthropicAPIKey) == "" {
		log.Warn("ai_rec: enabled but ANTHROPIC_API_KEY is empty — service disabled")
		return nil
	}
	if contextBuilder == nil || resolver == nil || quota == nil || chips == nil {
		log.Warn("ai_rec: missing collaborators — service disabled")
		return nil
	}

	// Set the prompt-caching beta header at client level. Caching is GA
	// for the older Claude families but newer models (e.g. claude-sonnet-4-6)
	// have been observed to silently ignore cache_control unless this
	// header is present. The header is harmless on models that don't
	// require it — it just enables a feature flag the server can opt into.
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.AnthropicAPIKey),
		option.WithHeader("anthropic-beta", "prompt-caching-2024-07-31"),
	)

	freeModel := cfg.ResolveModel(TierFree)
	paidModel := cfg.ResolveModel(TierPaid)
	chipsModel := cfg.ResolveChipsModel()

	s := &ClaudeService{
		cfg:           cfg,
		client:        &client,
		freeModel:     anthropic.Model(freeModel),
		paidModel:     anthropic.Model(paidModel),
		chipsModel:    anthropic.Model(chipsModel),
		context:       contextBuilder,
		resolver:      resolver,
		quota:         quota,
		chips:         chips,
		freshReleases: freshReleases,
	}
	log.WithFields(log.Fields{
		"free_model":  freeModel,
		"paid_model":  paidModel,
		"chips_model": chipsModel,
	}).Info("ai_rec: ClaudeService ready")
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
func (s *ClaudeService) GenerateChips(ctx context.Context, req ChipsRequest) (*ChipsResponse, error) {
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

func (s *ClaudeService) generateChipsUncached(ctx context.Context, tier Tier, uc *UserContext) (*ChipsResponse, error) {
	prompt := userPromptForChips(uc, desiredChips)

	log.WithField("feature", "ai_rec").
		WithField("history_size", uc.HistorySize).
		WithField("day", uc.DayOfWeek).
		WithField("bucket", uc.TimeOfDay).
		Debug("generating chips")

	chips, err := s.callClaudeForChips(ctx, prompt, tier)
	if err != nil {
		return nil, err
	}
	return &ChipsResponse{
		Chips:       chips,
		GeneratedAt: time.Now().Unix(),
		Tier:        tier.String(),
	}, nil
}

// --- Quota pass-through ---

// Remaining reports how many quota units the user has left today.
// Non-mutating; safe for GET handlers.
func (s *ClaudeService) Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Remaining(ctx, userID, tier)
}

// ConsumeQuota atomically charges one unit. Returns ErrQuotaExceeded if
// the user is already at their daily cap.
func (s *ClaudeService) ConsumeQuota(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Consume(ctx, userID, tier)
}

// DailyQuota returns the per-day request cap for the given tier — pure
// config lookup, no I/O. The UI uses it to render the remaining counter
// as "N / M" without a second round trip.
func (s *ClaudeService) DailyQuota(tier Tier) int {
	if tier == TierPaid {
		return s.cfg.PaidDailyQuota
	}
	return s.cfg.FreeDailyQuota
}

// QuotaResetAt returns the unix timestamp (seconds) at which the user's
// daily quota next rolls over. Delegates to the underlying Quota
// implementation, which keeps the "midnight UTC vs rolling 24h" decision
// in one place.
func (s *ClaudeService) QuotaResetAt() int64 {
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
func (s *ClaudeService) RecommendStream(ctx context.Context, req RecommendRequest, events chan<- StreamEvent) {
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

	// Phase 1: Claude is thinking. The UI swaps from idle to a "Claude
	// думает" indicator here. We stay in this phase until the FIRST
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
	//   streamClaudeItems  →  claudeCh  →  ResolveStreamFromChannel  →  recCh  →  SSE events
	//
	// The Claude streamer pushes a claudeItem onto claudeCh as soon as
	// the model has finished generating a `{title, year, reason}` triple
	// (typically every ~150-500ms depending on the model). The resolver
	// reads from claudeCh and fans out a TMDB lookup for each item
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
	claudeCh := make(chan claudeItem, claudeChannelBuffer)
	streamErrCh := make(chan error, 1)
	go func() {
		// streamClaudeItemsText (NDJSON / plain text) instead of the
		// tool_use streamClaudeItems — Anthropic buffers tool_use
		// generation server-side, so the latter doesn't actually flow
		// per-token. Plain text streams as it's generated.
		streamErrCh <- s.streamClaudeItemsText(ctx, prompt, req.History, req.Tier, claudeCh)
	}()

	recCh := make(chan Recommendation, r2BufferSize)
	go s.resolver.ResolveStreamFromChannel(ctx, claudeCh, models.ContentTypeMovie, req.Locale, recCh)

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
		log.WithError(err).WithField("feature", "ai_rec").Error("claude stream failed")
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

// claudeChannelBuffer is the buffer size for the streamClaudeItems → resolver
// hand-off. Just big enough that Claude doesn't stall on a slow resolver
// goroutine, small enough that cancel propagates quickly.
const claudeChannelBuffer = 8

// r2BufferSize sets the buffer for the resolver→stream channel. Just big
// enough to absorb a burst from a freshly-warmed TMDB cache without
// blocking goroutines, but small enough that cancel propagates fast.
const r2BufferSize = 4

// isAlreadyKnown reports whether the user has already engaged with the given
// videoID via either the watched-list or the watchlist. Used by the streaming
// pipeline to drop hallucinated duplicates Claude leaks past the prompt-side
// exclusion. Soft-fails to "unknown" on DB error so a transient blip never
// blocks a recommendation.
func (s *ClaudeService) isAlreadyKnown(ctx context.Context, userID uuid.UUID, videoID string) bool {
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

// --- Claude wire-level ---

// buildHistoryMessages converts our internal Message history into the SDK's
// MessageParam slice and appends the new user prompt as the final turn.
func buildHistoryMessages(history []Message, userPrompt string) []anthropic.MessageParam {
	messages := make([]anthropic.MessageParam, 0, len(history)+1)
	for _, m := range history {
		switch m.Role {
		case "user":
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)))
	return messages
}

// streamClaudeItemsText is the streaming Claude flow used by RecommendStream.
// It asks Claude for plain-text NDJSON output (one self-contained JSON
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
//     json.Unmarshal into claudeItem; malformed entries get logged and
//     dropped, the rest survive.
func (s *ClaudeService) streamClaudeItemsText(ctx context.Context, userPrompt string, history []Message, tier Tier, out chan<- claudeItem) error {
	defer close(out)

	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
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
	systemBlocks := []anthropic.TextBlockParam{
		{
			Text:         systemPromptNDJSON,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		},
	}
	if s.freshReleases != nil {
		if block := s.freshReleases.LoadFreshReleases(ctx); block != "" {
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text:         block,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			})
		}
	}

	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:       s.modelFor(tier),
		MaxTokens:   claudeMaxTokensRecommend,
		System:      systemBlocks,
		Messages:    messages,
		Temperature: anthropic.Float(0.7),
	})
	defer stream.Close()

	// The bracket-balance scanner emits each top-level JSON object as
	// soon as the closing brace lands.
	extractor := newNDJSONItemsExtractor(func(raw json.RawMessage) {
		var item claudeItem
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
	var (
		inputTokens       int64
		outputTokens      int64
		cacheCreateTokens int64
		cacheReadTokens   int64
		modelName         string
		ttftLogged        bool
		deltaCount        int
	)

	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "message_start":
			// message_start carries the initial Usage snapshot. The cache
			// fields here are sometimes already populated, sometimes not
			// (Anthropic seems to send the final cumulative values via
			// message_delta) — we read both and the message_delta loop
			// below overwrites if it has a fresher value.
			if event.Message.Usage.InputTokens > 0 {
				inputTokens = event.Message.Usage.InputTokens
			}
			if event.Message.Usage.CacheCreationInputTokens > 0 {
				cacheCreateTokens = event.Message.Usage.CacheCreationInputTokens
			}
			if event.Message.Usage.CacheReadInputTokens > 0 {
				cacheReadTokens = event.Message.Usage.CacheReadInputTokens
			}
			modelName = string(event.Message.Model)
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				deltaCount++
				if !ttftLogged {
					ttftLogged = true
					log.WithFields(log.Fields{
						"feature":     "ai_rec",
						"mode":        "text",
						"ttft_ms":     time.Since(streamStart).Milliseconds(),
						"cache_read":  cacheReadTokens,
						"cache_write": cacheCreateTokens,
					}).Info("claude first delta")
				}
				extractor.write(event.Delta.Text)
			}
		case "message_delta":
			// message_delta carries the cumulative usage update — this is
			// where the final cache_read / cache_write counts actually
			// land. Earlier we were only reading from message_start and
			// missing them entirely, which is why cache_read/write looked
			// like 0 even when caching was working.
			if event.Usage.OutputTokens > 0 {
				outputTokens = event.Usage.OutputTokens
			}
			if event.Usage.InputTokens > 0 {
				inputTokens = event.Usage.InputTokens
			}
			if event.Usage.CacheCreationInputTokens > 0 {
				cacheCreateTokens = event.Usage.CacheCreationInputTokens
			}
			if event.Usage.CacheReadInputTokens > 0 {
				cacheReadTokens = event.Usage.CacheReadInputTokens
			}
		}
	}
	if err := stream.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			log.WithField("feature", "ai_rec").Debug("claude stream cancelled")
			return nil
		}
		return errors.Wrap(err, "claude stream failed")
	}

	log.WithFields(log.Fields{
		"feature":       "ai_rec",
		"kind":          "recommend",
		"mode":          "text",
		"model":         modelName,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"cache_read":    cacheReadTokens,
		"cache_write":   cacheCreateTokens,
		"deltas":        deltaCount,
		"total_ms":      time.Since(streamStart).Milliseconds(),
	}).Info("claude stream complete")

	return nil
}

// callClaudeForChips runs the chip-generation tool and returns the parsed
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
// system block — see streamClaudeItemsText for the pattern.
func (s *ClaudeService) callClaudeForChips(ctx context.Context, userPrompt string, tier Tier) ([]Chip, error) {
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()

	tool := anthropic.ToolParam{
		Name:        chipsToolName,
		Description: anthropic.String("Return a list of suggestion chips tailored to the user and the current moment."),
		InputSchema: anthropic.ToolInputSchemaParam{
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
		},
	}

	resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     s.chipsModel,
		MaxTokens: claudeMaxTokensChips,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice:  anthropic.ToolChoiceParamOfTool(chipsToolName),
		Temperature: anthropic.Float(0.9),
	})
	if err != nil {
		return nil, errors.Wrap(err, "claude chips call failed")
	}

	s.logUsage(resp, "chips")

	input, err := extractToolUseInput(resp.Content, chipsToolName)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Chips []struct {
			Label string `json:"label"`
			Icon  string `json:"icon"`
			Query string `json:"query"`
		} `json:"chips"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, errors.Wrap(err, "claude chips: invalid tool input json")
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

// extractToolUseInput walks the response content blocks and returns the raw
// JSON input of the first tool_use block matching the expected tool name.
// Claude may interleave explanatory text blocks before the tool call despite
// the tool_choice forcing, so we cannot just index into Content[0].
func extractToolUseInput(blocks []anthropic.ContentBlockUnion, toolName string) (json.RawMessage, error) {
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name == toolName {
			if len(b.Input) == 0 {
				return nil, errors.Errorf("claude returned empty tool input for %s", toolName)
			}
			return json.RawMessage(b.Input), nil
		}
	}
	return nil, errors.Errorf("claude did not call tool %s (blocks=%d)", toolName, len(blocks))
}

// logUsage writes token usage at info level so we can aggregate costs in
// Loki without a separate metrics pipeline.
func (s *ClaudeService) logUsage(resp *anthropic.Message, kind string) {
	log.WithFields(log.Fields{
		"feature":       "ai_rec",
		"kind":          kind,
		"model":         resp.Model,
		"input_tokens":  resp.Usage.InputTokens,
		"output_tokens": resp.Usage.OutputTokens,
		"stop_reason":   resp.StopReason,
	}).Info("claude call complete")
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
