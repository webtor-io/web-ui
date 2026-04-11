# AI Recommendations (Discover)

Natural-language movie recommendations powered by Claude, surfaced as a
dedicated section at the top of `/discover`. Users describe what they want
(or tap a pre-generated suggestion chip) and get back a streaming grid of
cards, each with a short personalized reason explaining why it fits.

> Status: opt-in via env flag (`AI_RECOMMENDATIONS_ENABLED=true`).
> Paid users get a daily cap of 100 requests; free users get 1/day and see an
> upgrade CTA once exhausted.

## High-level flow

```
Discover mount
  └→ GET /discover/ai/chips                  (Redis 4h TTL, no quota)
     └→ 6 chips appear
        │  • zero-history user → static set from default_chips.go
        │  • everyone else     → Claude-generated, then cached
        └→ user taps chip OR types custom query
           └→ EventSource: GET /discover/ai/recommend/stream
              ├→ consume 1 quota unit (Redis Lua INCR, atomic)
              ├→ load watch history (movie_status ⋈ movie_metadata)
              ├→ open Claude streaming Messages.NewStreaming
              │  └→ stream NDJSON {title, year, reason} per token
              ├→ resolver fans out concurrent TMDB → OMDB → KP lookups
              │  └→ each resolved card emits an SSE 'item' event
              ├→ already-watched filter drops anything the user has seen
              └→ terminal SSE 'done' with quota / tier
           └→ user taps card
              └→ existing StreamModal flow (fetchMeta + fetchStreams via Stremio addons)
```

## Why streaming?

The pipeline (Claude generation + concurrent metadata lookups) takes 10-30
seconds end-to-end. Waiting that long for any first paint kills perceived
responsiveness. Instead we stream:

- The first `phase=claude` event hits the wire as soon as quota is
  consumed (~5ms after the request lands).
- The first card lands ~1-3 seconds later (Claude TTFT + first item
  completion + first TMDB lookup, all overlapped).
- Subsequent cards trickle in every 200-800ms.

The Anthropic SDK is used in **plain-text NDJSON** mode rather than
`tool_use` for the recommend path. Tool_use streaming is buffered
server-side at Anthropic — the entire JSON arrives as a single chunk for
many models, defeating the whole point. Plain text genuinely flows
token-by-token. The `ndjsonItemsExtractor` (a tiny brace-balance scanner
in `partial_json.go`) emits each top-level object the moment its closing
brace lands.

Chips still use `tool_use` because they're a single-shot non-streaming
call and we want Anthropic-side schema validation on the chip array shape.

## Backend architecture

### Services

- **`services/recommendations/`** — the whole pipeline.
  - `config.go` — CLI flags, `Config` struct, per-tier model resolver.
  - `service.go` — public types (`Chip`, `Recommendation`, `Message`,
    `RecommendRequest`, `ChipsRequest`, `Tier`, `StreamEvent` and
    payloads) and the `Service` interface.
  - `claude.go` — `ClaudeService` wiring Claude SDK, context builder,
    resolver, quota and chips cache. Hosts `RecommendStream`,
    `streamClaudeItemsText`, `callClaudeForChips`.
  - `prompt.go` — system prompts and per-mode user prompt builders.
  - `default_chips.go` — static chip sets for cold-start (zero-history)
    users; bypasses Claude entirely.
  - `partial_json.go` — `ndjsonItemsExtractor` brace-balance scanner.
  - `context.go` — `ClientClock`, `UserContext`, `UserContextBuilder`,
    `DBUserHistoryLoader` and history rendering helpers.
  - `resolver.go` — `Resolver` with three flavours: `Resolve` (slice,
    used by tests), `ResolveStream` (slice → channel), and
    `ResolveStreamFromChannel` (channel → channel — what production uses).
  - `quota.go` — `RedisQuota` with atomic Lua INCR + daily TTL.
  - `cache.go` — `ChipsCache` interface + `RedisChipsCache` implementation.
- **`services/enrich/enrich.go`** — `LookupByTitleYear(ctx, title, year, contentType)`
  is a public wrapper over the metadata mapper loop, keeping the
  recommender provider-agnostic.
- **`models/movie_status.go`** — `RatedMovie` flat struct,
  `ListUserRatedMovies(ctx, db, userID, limit)` joining `movie_status`
  with `movie_metadata` for prompt grounding, and `FilterWatchedMovieIDs`
  for the post-resolver dedup.

### Handler

- **`handlers/discover_ai/handler.go`** — gin handler behind `auth.HasAuth`.
  Maps service sentinel errors to HTTP codes and SSE error frames:

    | Sentinel | HTTP code | SSE `error` code |
    |---|---|---|
    | `ErrQuotaExceeded` | 402 `quota_exceeded` | `quota_exceeded` |
    | `ErrEmptyQuery` | 400 `empty_query` | `empty_query` |
    | `ErrQueryTooLong` | 400 `query_too_long` | `query_too_long` |
    | `ErrNoChips` (chips path only) | 200 with empty list | n/a |
    | upstream / unknown | 500 `internal` | `claude_failed` / `internal` |

There is **no** `ErrFeatureDisabled` sentinel — the disabled state is
expressed by `rec.New` returning a nil `Service` before the handler is
registered, so handlers never see the disabled case at runtime.

`ErrNoChips` is **only** raised by the chips path. On the recommend
path "0 items" is not an error — `RecommendStream` emits a normal
`done` event with `Total=0` and the UI shows its empty state.

### Wiring (serve.go)

```go
recSvc := rec.New(c, pg, redis, en)
if recSvc != nil {
    discover_ai.RegisterHandler(r, recSvc)
}
```

`rec.New` (in `services/recommendations/factory.go`) is the single
production wiring entry point — it constructs the config, history
loader, context builder, resolver, quota and chips cache, and hands
them to `NewClaudeService`. Tests should keep calling `NewClaudeService`
directly with mocks.

`rec.New` returns interface-nil when the feature flag is off or
`ANTHROPIC_API_KEY` is empty. In that case `serve.go` skips registration
entirely — the routes don't exist, gin returns its default 404, and the
Discover frontend reads that as "feature disabled" and hides the section.

Resolver concurrency is **10** (constant `resolverConcurrency` in
`factory.go`): Claude returns 6-10 candidates per request and we want
them all resolved in a single TMDB wave. Even 10 × ~3 HTTP calls per
item leaves us comfortably under TMDB's 40-req/10s burst limit.

## HTTP API

All routes require auth (`auth.HasAuth`).

### `GET /discover/ai/chips`

Returns the cached chip list for the user. Cache key is
`(userID, locale, day-of-week, time-of-day bucket)` — so "evening Monday"
chips don't leak into "morning Tuesday". TTL is 4h
(`AI_RECOMMENDATIONS_CHIPS_TTL_SECONDS`).

**Does not consume quota.** Free users can land on `/discover` without
burning their daily allowance. Cold-start users (zero watch history) get
a static curated chip set from `default_chips.go`, with no Claude call.

Query parameters (browser-supplied):

- `day` — English weekday name (`Intl.DateTimeFormat("en-US", {weekday: "long"})`)
- `hour` — local hour 0..23
- `locale` — `ru` or `en`

Response:

```json
{
  "chips": [
    {"id": "a1b2c3d4e5f6", "label": "😴 Чтоб уснуть в понедельник", "icon": "😴",
     "query": "Спокойные, медленные, сонные фильмы для понедельнего вечера..."}
  ],
  "generated_at": 1712001234,
  "tier": "paid",
  "remaining_quota": 98
}
```

### `POST /discover/ai/chips/refresh`

Bypasses the Redis cache and asks Claude for a new chip set.
**Consumes 1 quota unit.**

The frontend trigger is currently **commented out** in `AISection.jsx` —
the manual refresh button is hidden to keep accidental quota spend down.
The endpoint is still wired so re-enabling the button is a one-line
revert.

Body (JSON, with query-param fallback for curl / probes):

```json
{"locale": "ru", "clock": {"day": "Monday", "hour": 20}}
```

### `GET /discover/ai/recommend/stream`

SSE endpoint. Opens the streaming pipeline and emits `text/event-stream`
frames as cards become ready. The browser uses native `EventSource`.

Query parameters (everything goes via the URL because `EventSource` is
GET-only and cannot set custom headers):

- `query` — the user's natural-language request
- `locale`, `day`, `hour` — same as `/chips`
- `_csrf` — CSRF token (cannot use the `X-CSRF-TOKEN` header on EventSource)

**Headers explicitly set by the handler before `c.Stream`:**
`Content-Type: text/event-stream`, `Cache-Control: no-cache,no-store,no-transform`,
`Connection: keep-alive`, `X-Accel-Buffering: no`. Without these, the
webpack-dev-server proxy in dev (and various intermediates in prod)
buffer the response and break per-event delivery.

**Events:**

| Type | Payload | When |
|---|---|---|
| `phase` | `{"phase": "claude" \| "resolving", "expected"?: int}` | Pipeline stage transition |
| `item` | `{"video_id", "title", "year", "poster", "plot", "rating", "reason", "type"}` | Each resolved card |
| `done` | `{"total", "remaining_quota", "tier"}` | Terminal success |
| `error` | `{"code", "tier"?}` | Terminal failure |

The stream is terminated by either `done` or `error`; the client closes
the EventSource on receipt.

### `GET /discover/ai/refine/stream`

Same shape as `/recommend/stream` plus a `history` query parameter — a
JSON-encoded array of `{role, content}` turns. The client caps history
to the last 4 turns (`HISTORY_TURNS_CAP` in `aiClient.js`) to keep URL
length comfortably under proxy limits.

Consumes 1 quota unit on equal footing with a fresh `/recommend/stream`.

The refine prompt **re-renders the user's current watch-history block**
(see `userPromptForRefine` in `prompt.go`) so freshly-watched titles are
honoured — without this, refine would only know "Claude's previous
suggestions", not "what the user has actually watched / rated since".

## Prompting strategy

### Two distinct system prompts

`prompt.go` defines two system prompts:

- **`systemPromptNDJSON`** (~2500 tokens) — used by the streaming
  recommend / refine path. Sized to clear Anthropic's prompt-caching
  minimums (1024 tokens for Sonnet, 2048 for Haiku) so `cache_control`
  on the system block actually activates; subsequent calls within ~5
  minutes get TTFT cut 3-5× and cached input billed at ~10% of normal.
  The bulk is not padding — it's few-shot examples (good/bad reason
  pairs in EN and RU), genre vocabulary, common-pitfalls section, and
  strict NDJSON output rules.
- **`systemPrompt`** (~250 tokens) — used by the chips path. Tool_use
  mode, Redis-cached on our side for 4h. Provider-side prompt caching
  is **intentionally NOT enabled** here: 250 tokens is below the
  caching minimum, and the Redis layer absorbs >95% of chip requests
  anyway. See the comment block above `callClaudeForChips` for the
  recipe to enable it later.

### Output format

- **Recommend / refine** — plain-text NDJSON: one self-contained
  `{"title", "year", "reason"}` JSON object per line, no array wrapper.
  Parsed incrementally by `ndjsonItemsExtractor`. Final validity is
  enforced by `json.Unmarshal` into `claudeItem`; malformed entries get
  logged and dropped, the rest survive.
- **Chips** — `tool_use` with the `return_chips` schema. Tool_use here
  is fine because (a) chips are a single-shot non-streaming call,
  (b) we want schema validation on the chip array shape.

### Title + year only

We ask Claude for real titles and years, *not* IMDB ids (which the model
will happily hallucinate). The backend resolves each tuple against the
real metadata chain; Claude-only items that can't be resolved are
silently dropped. The UI sees only verified-by-TMDB cards.

### No assistant message prefill

It would be tempting to force the first output character with an
assistant prefill of `{`, but Sonnet 4.x explicitly rejects requests
where the conversation ends with an assistant turn. We rely on the
strict system prompt instead, and the NDJSON scanner gracefully ignores
any commentary before the first `{`.

### Temperature

- 0.7 for recommendations (enough variety without going off the rails)
- 0.9 for chips (witty, unexpected)

## Streaming pipeline internals

```
streamClaudeItemsText ──claudeCh──→ ResolveStreamFromChannel ──recCh──→ SSE handler ──→ wire
   (Claude stream)                  (concurrent TMDB lookups)
```

Three goroutines per request:

1. **Claude streamer** — reads SSE events from Anthropic, runs the
   partial-JSON scanner, pushes complete `claudeItem`s onto `claudeCh`.
2. **Resolver fan-out** — reads from `claudeCh`, kicks off a TMDB lookup
   for each item (semaphore-bounded at 10), pushes resolved
   `Recommendation`s onto `recCh`.
3. **gin Stream callback** — reads from the service's `events` channel
   and turns each event into an SSE frame on the wire.

**Channel close discipline.** Each goroutine `defer close()`s its own
output channel. The SSE handler only reads — it never closes anything.

**Cancellation.** All goroutines descend from `c.Request.Context()`.
On client disconnect:

- The Claude HTTP stream is torn down (its derived ctx is cancelled).
- Resolver goroutines mid-flight exit via their `case <-ctx.Done()`
  branches.
- The service does NOT `return` early on a failed `send()`; it keeps
  draining `recCh` so resolver goroutines that are mid-send don't block
  forever — they then self-exit on ctx.

**Trade-off accepted on disconnect:** the final `message_delta` from
Anthropic (which carries `cache_read` / `cache_write` counts) is lost.
We considered detaching the Claude ctx from the request ctx to get those
metrics back, but that creates orphan streams that keep burning tokens
after the user is gone — net negative.

**No early Claude cancellation on "enough items".** We let the Claude
stream run to natural completion specifically so the `message_delta`
event arrives on the happy path, which is the only place final cache
usage tokens land. Cancelling would save ~100 output tokens but break
observability of caching.

## Metadata resolution

The resolver is provider-agnostic. It calls
`enrich.Enricher.LookupByTitleYear`, which iterates through TMDB → OMDB
→ Kinopoisk in order. The first mapper that finds a match wins.

**Non-IMDB results are dropped.** A mapper may return a metadata entry
with a `tmdbXXX` or `kpXXX` identifier when no IMDB id can be found;
these can't be streamed through our Stremio addons (which only know
`tt*` ids), so the resolver discards them with a warning. We prefer
fewer working cards over more broken ones.

**Already-watched filter.** After resolution, each card is checked
against `FilterWatchedMovieIDs` for the current user and dropped if the
user has marked it watched. The system prompt also instructs Claude to
skip the user's history, but we don't trust the model to respect that
perfectly. Single-row pg lookup per item, ~1ms each — batching not
worth the complexity.

**Side benefit:** AI recommendation lookups warm the per-mapper caches
(`tmdb.info`, `tmdb.query`, `omdb.info`, ...). A user who later opens a
torrent for one of those films gets instant enrichment at no extra API
cost.

## Quota

Backed by Redis via a tiny Lua script in `quota.go`. Key layout:

```
ai_rec:q:{userID}:{YYYY-MM-DD}
```

The script `INCR`s, sets `EXPIRE` on the first hit (TTL = seconds until
end of UTC day, minimum 60s), and `DECR`s back if the user is over their
limit — all in a single round trip, so a double-click can never mint
free quota. The key naturally rolls over at midnight UTC without a
scheduled cleanup job.

Defaults:

| Tier | Daily Limit | Flag |
|---|---|---|
| free | 1 | `AI_RECOMMENDATIONS_FREE_DAILY_QUOTA` |
| paid | 100 | `AI_RECOMMENDATIONS_PAID_DAILY_QUOTA` |

`100` for paid is an anti-abuse cap, not a budget target.

**Quota is consumed BEFORE the Claude call.** This means a transient
Anthropic 5xx burns the user's slot. We considered refunding on
`internal` failures but rejected it for race-safety: the current
ordering guarantees no quota state ambiguity. Free-tier users losing
their single daily slot to an upstream failure is an accepted
trade-off.

## Caching

- **Chips:** distributed Redis cache (`RedisChipsCache`). *Must* be
  distributed because web-ui runs multiple replicas behind a load
  balancer, and a lazymap-backed cache on pod A would be invisible to
  pod B. Key includes locale + day + time-of-day bucket — chips rotate
  naturally as the day progresses.
- **Recommendations:** not cached server-side. Every query is unique
  per `(query, history)`, and the daily quota is the primary rate
  limiter. Skipping the cache removes a whole class of double-consume
  races and keeps the code trivial.
- **Anthropic prompt caching** (provider side): enabled on the system
  block of `streamClaudeItemsText` via `cache_control: ephemeral`. The
  system prompt is sized past the 1024/2048-token minimums so caching
  actually activates. **Not** enabled on the chips path — see
  Prompting strategy for the rationale.
- **Metadata lookups:** already cached inside each mapper
  (`tmdb.query`, `omdb.info`, ...) — unchanged by this feature.

## Configuration

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--ai-recommendations-enabled` | `AI_RECOMMENDATIONS_ENABLED` | `false` | Master kill switch |
| `--anthropic-api-key` | `ANTHROPIC_API_KEY` | `""` | Required when enabled |
| `--ai-recommendations-model` | `AI_RECOMMENDATIONS_MODEL` | `claude-haiku-4-5-20251001` | Legacy single-model fallback |
| `--ai-recommendations-free-model` | `AI_RECOMMENDATIONS_FREE_MODEL` | (inherits) | Free-tier override |
| `--ai-recommendations-paid-model` | `AI_RECOMMENDATIONS_PAID_MODEL` | (inherits) | Paid-tier override (e.g. Sonnet) |
| `--ai-recommendations-free-daily-quota` | `AI_RECOMMENDATIONS_FREE_DAILY_QUOTA` | `1` | Free tier cap |
| `--ai-recommendations-paid-daily-quota` | `AI_RECOMMENDATIONS_PAID_DAILY_QUOTA` | `100` | Paid tier cap |
| `--ai-recommendations-max-query-length` | `AI_RECOMMENDATIONS_MAX_QUERY_LENGTH` | `500` | Sanitisation guard |
| `--ai-recommendations-history-limit` | `AI_RECOMMENDATIONS_HISTORY_LIMIT` | `40` | Rows fed into the prompt |
| `--ai-recommendations-chips-ttl-seconds` | `AI_RECOMMENDATIONS_CHIPS_TTL_SECONDS` | `14400` | 4h Redis TTL |
| `--ai-recommendations-recs-ttl-seconds` | `AI_RECOMMENDATIONS_RECS_TTL_SECONDS` | `1800` | reserved (currently unused) |

The free / paid model split lets paid users be routed to a smarter model
(e.g. Sonnet) while free users stay on Haiku for cost. If only
`--ai-recommendations-model` is set, both tiers use it.

## Frontend

- **`assets/src/js/lib/discover/aiClient.js`** — `fetch` wrapper for
  chips and a native `EventSource` wrapper (`recommendStream` /
  `refineStream`) for the streaming endpoints. Throws a typed `AIError`
  on failures. Caps history to 4 turns before encoding into the URL.
- **`assets/src/js/lib/discover/components/discoverReducer.js`** — `ai`
  slice on the existing discover state. Phases:
  - `disabled`, `idle`, `loadingChips`, `chipsReady`, `chipsError`
  - `streamingClaude`, `streamingResolve`, `recsReady`, `recsError`
  - `quotaExceeded`
  - Streaming actions: `AI_STREAM_START`, `AI_STREAM_PHASE`,
    `AI_STREAM_ITEM`, `AI_STREAM_DONE`, `AI_STREAM_ERROR`,
    `AI_EXPAND_RECS`, `AI_QUOTA_EXCEEDED`, `AI_RESET`.
- **`assets/src/js/lib/discover/components/ai/`** — Preact components:
  - `AISection.jsx` — top container; runs the EventSource lifecycle,
    manages phase copy, swaps between query input and refine input.
  - `AIChipsRow.jsx` — chip pill list.
  - `AIQueryInput.jsx` — free-form input shared by initial / refine
    modes (different placeholder + button copy).
  - `AIRecsGrid.jsx` — chessboard layout (alternating poster-left /
    poster-right rows). First 4 cards visible; "Show N more" button
    reveals the rest.
  - `AIRecCard.jsx` — poster-only card with watched / rating badges
    reused from `ItemGrid`.
- **Bridge to existing flow:** `DiscoverApp.jsx:handleAICardClick` maps
  an AI recommendation onto the catalog-item shape that `cardClick`
  already handles, reusing `StreamModal` verbatim. Same trick for
  `handleAIToggleWatched` and `handleAIOpenRating`.

### Clock / locale

The browser sends its local weekday and 0..23 hour, **not** a UTC
timestamp. The server is UTC and cannot infer the user's timezone;
"Monday evening" needs to match the user's intuition regardless of
where they are.

If the client sends an invalid or missing clock (or a malformed `hour`
query param), the server falls back to a neutral "Saturday afternoon"
window rather than guessing from UTC.

### UIKit

The section uses the cyan theme (secondary actions):

- `bg-w-cyan/10 text-w-cyan border-w-cyan/30` for chip pills and the
  "Show more" button
- `text-w-cyan italic` on the card reason block with a left border
  stripe
- No new CSS classes are introduced

## Privacy

Claude receives: title, year, and rating signal for the user's most
recent watched/rated movies (default 40 entries). **No email, user id,
watch position, or other PII is sent.** The user id is used only as a
Redis key for quota / chip cache; it never leaves our infrastructure.

Users can effectively opt out today by never triggering the AI section
— chips load on visit but no Claude call happens until they tap a chip
or submit text. Cold-start users (zero history) get static chips, no
Claude call at all. If a future requirement is a hard opt-out toggle,
add it as a column on `users` and check it in the handler before
calling `RecommendStream`.

## Rollout plan

1. Feature-flag the deploy (`AI_RECOMMENDATIONS_ENABLED=false`) on stage.
2. Set `ANTHROPIC_API_KEY` and flip the flag on stage; smoke test
   end-to-end **including the SSE streaming path through any HTTP proxy
   in front of you** (the buffering invariants are easy to break).
3. Monitor Loki logs on `feature=ai_rec`: token usage, resolver drop
   rate, Claude error rate, `cache_read` / `cache_write` ratios on the
   recommend path (the latter is how you confirm prompt caching is
   actually working).
4. Flip on prod once stage metrics look sane.
5. Watch daily Anthropic billing for the first week; if it trends high,
   lower `AI_RECOMMENDATIONS_PAID_DAILY_QUOTA` and/or downgrade the
   paid model.

## Known follow-ups

- **Stremio availability pre-filter.** Today we show every resolved
  card and let `StreamModal` surface "no streams" on click. If the drop
  rate observed in prod is > 20%, add a parallel Cinemeta check in the
  resolver and prune accordingly.
- **Cross-mapper IMDB resolution.** Kinopoisk / OMDB may return a
  TMDB-only or KP-only entry that we currently drop. A future
  enhancement is to re-resolve those through TMDB to recover a `tt*` id.
- **Series recommendations.** Today the resolver forces
  `ContentTypeMovie`. Supporting series requires Claude to label the
  type per item and the StreamModal to open into the episode picker
  instead of the movie path.
- **Prometheus metrics.**
  `ai_recommendations_requests_total{kind, outcome}`,
  `ai_recommendations_tokens_total{direction}`,
  `ai_recommendations_latency_seconds`. Today the metric pipeline is
  "log-and-aggregate-in-Loki"; if we ever need real-time dashboards,
  ship structured Prometheus counters.
- **`claude-sonnet-4-6` cache support.** Sonnet 4.6 silently ignores
  `cache_control` (verified empirically — Anthropic returns 0/0 for
  cache fields). Use `claude-sonnet-4-5-20250929` if caching matters
  for the paid-tier model. Re-test when Anthropic ships a fix.
- **End-to-end streaming pipeline test.** `parser`, `resolver`, `quota`
  and `context` all have unit tests, but the streaming `RecommendStream`
  path is only exercised manually. A fake Anthropic SSE producer would
  give us coverage on the goroutine choreography and cancel paths.
