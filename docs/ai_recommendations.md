# AI Recommendations (Discover)

Natural-language movie recommendations powered by Anthropic or OpenAI, surfaced as a
dedicated section at the top of `/discover`. Users describe what they want
(or tap a pre-generated suggestion chip) and get back a streaming grid of
cards, each with a short personalized reason explaining why it fits.

> Status: opt-in via env flag (`AI_RECOMMENDATIONS_ENABLED=true`).
> Paid users get a daily cap of 100 requests; free users get 1/day and see an
> upgrade CTA once exhausted.

## AI provider selection

`services/ai_client.New(c)` selects the shared provider from configured credentials:

| Credentials | Provider | Default model |
|---|---|---|
| `ANTHROPIC_API_KEY` only | Anthropic Messages API | `claude-haiku-4-5-20251001` |
| `OPENAI_API_KEY` only | OpenAI Responses API | `gpt-4.1-mini` |
| Both keys | Anthropic (preserves existing deployments; logged at startup) | `claude-haiku-4-5-20251001` |
| Neither, or whitespace-only keys | No AI client; feature routes and UI are disabled | — |

Selection uses configuration, never an API health probe. A failed request stays
with its selected provider; there is no cross-provider retry. Feature flags still
control whether recommendations and enrichment are enabled. The same client is
used by `serve`, `enrich run`, `enrich popular`, and the subscription worker.

Model overrides (`AI_RECOMMENDATIONS_MODEL`, `AI_RECOMMENDATIONS_FREE_MODEL`,
`AI_RECOMMENDATIONS_PAID_MODEL`, `AI_RECOMMENDATIONS_CHIPS_MODEL`) must belong to
the selected provider. If unset, defaults are resolved after provider selection.
An explicitly configured Claude model is not silently rewritten to a GPT model.

To use OpenAI with provider defaults, clear the Anthropic key and any old Claude
model overrides, then configure:

```dotenv
OPENAI_API_KEY=<your-key>
AI_RECOMMENDATIONS_ENABLED=true
AI_ENRICH_ENABLED=true
```

For an explicit OpenAI model split without a reasoning stage:

```dotenv
AI_RECOMMENDATIONS_FREE_MODEL=gpt-4.1-mini
AI_RECOMMENDATIONS_PAID_MODEL=gpt-4.1
AI_RECOMMENDATIONS_CHIPS_MODEL=gpt-4.1-mini
AI_ENRICH_MODEL=gpt-4.1-mini
```

This is an initial operating profile, not a measured quality equivalence to
Haiku / Sonnet. If both keys remain configured, Anthropic still wins even when
the model overrides name OpenAI models: clear the active Anthropic key when
switching this profile.

`OPENAI_BASE_URL` / `--openai-base-url` optionally changes the API base URL
(default `https://api.openai.com/v1`); the endpoint must support Responses API,
including function calls and SSE. OpenAI uses stateless requests with `store=false`
and the model's default sampling; temperature is omitted because reasoning models
reject it. The existing output-token limits also include reasoning tokens for
OpenAI: choosing a reasoning model may require revisiting those limits and latency.

Both providers use the same prompts, NDJSON parser, quota, metadata resolver,
Redis cache and browser SSE protocol. Historical wire names `phase=claude` and
`claude_failed` remain for compatibility; logs identify the actual `provider`.
Anthropic retains its system-block cache breakpoints; OpenAI joins those blocks
into `instructions` and reports automatic prefix-cache usage on completion.

API contract references: [OpenAI text generation](https://developers.openai.com/api/docs/guides/text),
[function calling](https://developers.openai.com/api/docs/guides/function-calling),
[streaming responses](https://developers.openai.com/api/docs/guides/streaming-responses).

Helm chart values: `anthropic.apiKey`, `openai.apiKey`, `openai.baseURL`; the
private chart injects both keys when either AI feature is enabled. Leave its model
settings empty to use provider defaults. Chart changes live in `infra/helmfile`.

A successful OpenAI `GET /v1/models` validates authentication and lists models,
but does not prove that generation is funded. Responses requests can still fail
with HTTP 429 or a stream error containing `insufficient_quota` /
`credit_balance_exhausted`. Check the API project's billing balance and limits;
this is distinct from a temporary requests-per-minute limit. The selected
provider stays unchanged. Verify a real Responses request before switching a
working deployment to OpenAI.

### Live adapter check (2026-10-01)

Ten live scenarios passed: recommendations from GPT-4.1 Mini, GPT-4.1, Haiku 4.5
and Sonnet 4.5; OpenAI chips via both tool calls and text streaming; two title
normalizations each on OpenAI and Anthropic. OpenAI successfully returned final
usage and complete recommendations before the stream ended.

One Russian recommendation query per model, with synthetic history / watchlist:

| Model | First parsed recommendation | Whole stream | Parsed recommendations |
|---|---:|---:|---:|
| `gpt-4.1-mini` | 2853 ms | 5125 ms | 7 |
| `gpt-4.1` | 2499 ms | 4852 ms | 8 |
| `claude-haiku-4-5-20251001` | 1533 ms | 4631 ms | 7 |
| `claude-sonnet-4-5-20250929` | 2457 ms | 9090 ms | 8 |

These are single-request observations, not percentiles or a quality benchmark.
The check calls the production adapters and recommendation / chip / enrichment
consumers. It does not run metadata resolution, database caches, the fresh-release
prompt block, the browser SSE handler or the production proxy. First parsed
recommendation timing therefore excludes card hydration and browser delivery.

### Prompt and model comparison (2026-10-02)

Exploratory direct Responses API probe: 45 completed responses, plus two TLS
failures retried once. Synthetic contexts only; production configuration and
prompts were not changed. The raw answers, exact prompts and summary are kept
outside the repository: they quote film titles.

Chips: one six-chip response for each of the 11 locales, per variant. Cases
include watchlist-only context and an instruction-injection attempt in a title.
Both candidates retain the intended rules; the conservative candidate only
removes the large full-example sets, retaining the original rules, bad/good
pairs and composition guidance.

| Chips prompt | Input tokens | Output tokens | Cost without cache discounts | Labels over 40 characters |
| --- | ---: | ---: | ---: | ---: |
| Current | 49,582 | 3,044 | $0.02470 | 18 / 66 |
| Aggressive rewrite | 7,980 | 3,889 | $0.00941 | 34 / 66 |
| Remove full-example sets | 27,197 | 3,040 | $0.01574 | 18 / 66 |

All completed responses parse as NDJSON. Aggressive shortening regressed label
length and naturalness: rejected. Conservative shortening reduced input by 45%
and uncached-equivalent cost by 36%, but did not establish quality equivalence:
watchlist references and structural-quota violations remain. It is an experiment
candidate, not a replacement production prompt. Baseline also violates quotas.

Recommendations: GPT-4.1 and mini each answered six identical scenarios with
the unchanged recommendation prompt and a fixed eight-title recent-release
fixture. Mini cost $0.01586 versus $0.08102, but violated explicit year bounds,
returned watched titles (including a localized alias), and confused film credits.
GPT-4.1 also made actor/genre mistakes; it is not a correctness oracle. Keep the
paid model unchanged. Production's resolved-ID watched/watchlist filter can
remove repeats, but cannot repair incorrect year/actor/genre reasoning.

These are single samples, not a statistical quality benchmark. The fixture is
not the production 200-film catalog. No metadata hydration, live availability,
Go consumer, browser or end-to-end SSE checks were run. Sequential requests warm
provider caches; compare uncached-equivalent costs above, not raw billed ratios.
Reported usage across completed calls corresponds to about $0.139; failed streams
may have consumed additional unreported tokens. No deployment followed this probe.

## High-level flow

```
Discover mount
  └→ GET /discover/ai/chips                  (Redis 4h TTL, no quota)
     └→ 6 chips appear
        │  • cold-start user (no history AND empty watchlist) → static set
        │    from default_chips.go
        │  • everyone else                                    → AI-
        │    generated, then cached
        └→ user taps chip OR types custom query
           └→ EventSource: GET /discover/ai/recommend/stream
              ├→ consume 1 quota unit (Redis Lua INCR, atomic)
              ├→ load watch history (movie_status ⋈ movie_metadata)
              ├→ load watchlist     (movie_watchlist + series_watchlist
              │                      merged newest-first, capped at limit)
              ├→ open shared AI client StreamText
              │  └→ stream NDJSON {title, year, reason} per token
              ├→ resolver fans out concurrent TMDB → OMDB → KP lookups
              │  └→ each resolved card emits an SSE 'item' event
              ├→ already-known filter drops anything the user has watched
              │  OR already saved to their watchlist
              └→ terminal SSE 'done' with quota / tier
           └→ user taps card
              └→ existing StreamModal flow (fetchMeta + fetchStreams via Stremio addons)
```

## Why streaming?

The original Anthropic pipeline (AI generation + concurrent metadata lookups) took 10-30
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

The non-streaming chips endpoint uses a forced tool call (`tool_use` on
Anthropic, `function_call` on OpenAI). Streaming chips use NDJSON text
through the same parser as recommendations.

## Backend architecture

### Services

- **`services/ai_client/`** — credential selection, shared request/tool/usage types,
  Anthropic Messages and OpenAI Responses SDK adapters.
- **`services/recommendations/`** — the whole pipeline.
  - `config.go` — CLI flags, `Config` struct, per-tier model resolver.
  - `service.go` — public types (`Chip`, `Recommendation`, `Message`,
    `RecommendRequest`, `ChipsRequest`, `Tier`, `StreamEvent` and
    payloads) and the `Service` interface.
  - `ai.go` — `AIService` wiring the shared AI client, context builder,
    resolver, quota and chips cache. Hosts `RecommendStream`,
    `streamAIItemsText`, `callAIForChips`.
  - `prompt.go` — system prompts and per-mode user prompt builders.
  - `default_chips.go` — static chip sets for cold-start (zero-history)
    users; bypasses the AI provider entirely.
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
- **`models/movie_watchlist.go` / `models/series_watchlist.go`** —
  `ListMovieWatchlistItems` / `ListSeriesWatchlistItems` joined with
  `*_metadata` are merged in `DBUserHistoryLoader.ListUserWatchlist` to
  feed Claude a unified "user has bookmarked these" block. The matching
  `FilterMovieWatchlistVideoIDs` / `FilterSeriesWatchlistVideoIDs`
  helpers back the post-resolver `isAlreadyKnown` filter so a Claude
  hallucination doesn't slip an already-saved title back into the grid.

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
aiClient := ai_client.New(c)
recSvc := rec.New(c, aiClient, pg, redis, en, en)
if recSvc != nil {
    discover_ai.RegisterHandler(r, recSvc)
}
```

`rec.New` (in `services/recommendations/factory.go`) is the single
production wiring entry point — it constructs the config, history
loader, context builder, resolver, quota and chips cache, and hands
them to `NewAIService`. Tests should keep calling `NewAIService`
directly with mocks.

`rec.New` returns interface-nil when the feature flag is off or
neither provider API key is configured. In that case `serve.go` skips registration
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
- `locale` — one of `en`, `ru`, `es`, `de`, `fr`, `pt`, `it`, `pl`, `tr`, `nl`, `cs` (see Localization below)

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
The watchlist block is re-rendered on the same path for the same reason —
a title bookmarked mid-conversation is excluded from the next refine
turn without waiting for cache invalidation.

### Watchlist as taste signal

Alongside the rated-history block, the prompt carries a "user has
bookmarked these titles" block populated from `movie_watchlist` +
`series_watchlist` (merged newest-first, capped at `HistoryLimit`).
Claude treats it both as exclusion (don't recommend something the user
already saved) and as taste grounding (e.g. a saved series shifts later
picks toward similar shows). A user with empty watch history but a
populated watchlist is no longer a cold-start case — chips skip the
static fallback and call Claude with the watchlist as the only personal
signal.

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
  anyway. See the comment block above `callAIForChips` for the
  recipe to enable it later.

### Output format

- **Recommend / refine** — plain-text NDJSON: one self-contained
  `{"title", "year", "reason"}` JSON object per line, no array wrapper.
  Parsed incrementally by `ndjsonItemsExtractor`. Final validity is
  enforced by `json.Unmarshal` into `recommendationItem`; malformed entries get
  logged and dropped, the rest survive.
- **Chips** — a forced tool call with the `return_chips` schema. A tool call here
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

Anthropic receives 0.7 for recommendations and 0.9 for chips. OpenAI requests
omit temperature and use the selected model's default sampling.

## Localization

The recommender is locale-aware. Reasons are written in the user's UI
language, and cold-start chips are served from per-locale curated sets.

### Supported locales

| Code | Language | Notes |
|------|----------|-------|
| `en` | English | Default and fallback for unknown locales |
| `ru` | Russian | Informal "ты" form |
| `es` | Spanish | Informal "tú", LATAM–Spain neutral |
| `de` | German | Informal "du" |
| `fr` | French | Formal "vous" (web convention) |
| `pt` | Portuguese | **Brazilian Portuguese (PT-BR)** under the bare `pt` code |
| `it` | Italian | Informal "tu" |
| `pl` | Polish | Informal "ty" |
| `tr` | Turkish | Impersonal/imperative forms preferred |
| `nl` | Dutch | Informal "je"/"jij" |
| `cs` | Czech | Informal "ty" |

`services/recommendations/context.go` `supportedLocales` is the source of
truth on the server. The JS client mirrors it via `SUPPORTED_LOCALES` in
`assets/src/js/lib/discover/aiClient.js` and must be kept in sync — when
the JS sends an unsupported value the server normalizes it to `en` and
serves English chips on a non-English UI, which is jarring.

### How locale flows from UI to Claude

1. The i18n middleware (`services/i18n/middleware.go`) sets the user's
   active language from URL prefix (`/ru/`, `/fr/`...), the `lang` cookie,
   or `Accept-Language` on first visit. It writes it into both
   `<html lang="...">` (server-rendered) and the cookie.
2. The discover JS reads `document.documentElement.lang` first
   (`aiClient.js currentLocale()`). It is the user's CURRENT explicit
   choice and trumps `navigator.languages`, which would otherwise overrule
   a switcher click. Browser preferences only matter as a fallback when
   the script runs outside the normal Webtor layout.
3. Each XHR/SSE request includes `locale=<code>` as a query param. The
   handler runs `normalizeLocale` to clamp it.
4. The locale is stamped into `UserContext`, used as part of the chips
   cache key (so locales don't share slots), and printed verbatim into
   the prompt's `Response locale: <code>` line.

### Cold-start chips per locale

`services/recommendations/default_chips.go` holds a `defaultChipDefs<XX>`
slice for every supported locale. To add a new locale:

1. Add the 2-letter code to `supportedLocales` in `context.go`.
2. Add a `defaultChipDefs<XX>` slice — translate the 6 EN labels and
   re-use the EN queries verbatim. The queries stay English on purpose:
   Claude reads the locale from `UserContext` and writes reasons in the
   right language regardless of input query language. Keeping queries in
   one language avoids vocabulary drift across cold-start sets.
3. Register the slice in `defaultChipDefsByLocale`.
4. Add a `<XX>` entry to `SUPPORTED_LOCALES` in `aiClient.js`.
5. Add a few-shot example for the locale in `systemPromptNDJSON`
   (Examples A–H block) — Claude leans heavily on these to land the
   right tone and avoid drifting into formal/marketing register on
   languages it sees less in our prompt.

The chip-cache key is salted with locale, so locale switches naturally
yield fresh chips — no manual invalidation required.

## Streaming pipeline internals

```
streamAIItemsText ──aiItemsCh──→ ResolveStreamFromChannel ──recCh──→ SSE handler ──→ wire
   (AI stream)                      (concurrent TMDB lookups)
```

Three goroutines per request:

1. **AI streamer** — receives text deltas from the shared provider adapter,
   runs the partial-JSON scanner, and pushes complete `recommendationItem`s
   onto `aiItemsCh`.
2. **Resolver fan-out** — reads from `aiItemsCh`, kicks off a TMDB lookup
   for each item (semaphore-bounded at 10), pushes resolved
   `Recommendation`s onto `recCh`.
3. **gin Stream callback** — reads from the service's `events` channel
   and turns each event into an SSE frame on the wire.

**Channel close discipline.** Each goroutine `defer close()`s its own
output channel. The SSE handler only reads — it never closes anything.

**Cancellation.** All goroutines descend from `c.Request.Context()`.
On client disconnect:

- The provider HTTP stream is torn down (its derived ctx is cancelled).
- Resolver goroutines mid-flight exit via their `case <-ctx.Done()`
  branches.
- The service does NOT `return` early on a failed `send()`; it keeps
  draining `recCh` so resolver goroutines that are mid-send don't block
  forever — they then self-exit on ctx.

**Trade-off accepted on disconnect:** final provider usage may be lost.
Detaching the provider context would leave streams burning tokens after
the user disconnects, so cancellation follows the request context.

**No early AI cancellation on "enough items".** The adapter drains the stream
to its terminal event and retains final token and cache usage. Anthropic
merges `message_start` / `message_delta` usage; OpenAI receives final usage
in `response.completed`.

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

**Quota is consumed BEFORE the AI call.** This means a transient
provider 5xx burns the user's slot. We considered refunding on
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
  block of `streamAIItemsText` via `cache_control: ephemeral`. The
  system prompt is sized past the 1024/2048-token minimums so caching
  actually activates. **Not** enabled on the chips path — see
  Prompting strategy for the rationale.
- **Metadata lookups:** already cached inside each mapper
  (`tmdb.query`, `omdb.info`, ...) — unchanged by this feature.
- **OpenAI prefix caching:** automatic on the provider side. The adapter
  reports cached input tokens from final response usage; it does not send
  Anthropic cache controls.

## Observability & cost

Two logrus lines carry per-call AI usage, both with `feature=ai_rec` and `provider=anthropic|openai`:

| Message | Emitted by | Key fields |
|---|---|---|
| `ai stream complete` | `streamAIItemsText` | `kind`, `model`, `input_tokens`, `output_tokens`, `cache_read`, `cache_write`, `history_size`, `watchlist_size`, `deltas`, `total_ms` |
| `ai chips stream complete` | `streamAIChipsText` | same, minus the history/watchlist pair |

`history_size` + `watchlist_size` exist to attribute spend to cold-start vs
personalised traffic: a call with both at zero is one where the prompt
carried no personal signal at all, so its result is in principle shareable
across users (see Known follow-ups). They are logged, not acted on.

Note `kind` is hardcoded to `recommend` on the completion line — refine
calls are indistinguishable there. The `quota charged` line does
distinguish them, so a refine ratio has to be derived from that.

**Measured Anthropic baseline (2026-07-29, 7-day window via Loki):**

| Path | Calls / week | ≈ $ / week | ≈ $ / call |
|---|---|---|---|
| `recommend` (Haiku, free) | 275 | 4.65 | 0.017 |
| `recommend` (Sonnet, paid) | 86 | 4.10 | 0.051 |
| `chips` (Haiku) | 635 | 4.10 | 0.007 |

~90% of that is the input prompt (≈15.1K tokens per `recommend` call vs
~330 output). Anthropic prompt caching is currently a **wash, not a win**:
at ~1.6 calls/hour the 5-minute ephemeral TTL expires between calls, so
the ×1.25 write premium cancels the ×0.1 read discount almost exactly. A
1-hour TTL (×2 write) would be strictly worse at this traffic level. Both
observations flip once Discover traffic grows roughly 7-8×; until then
prompt-size work has poor ROI relative to the regression risk in
`fresh_releases` (see Known follow-ups).

## Configuration

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--ai-recommendations-enabled` | `AI_RECOMMENDATIONS_ENABLED` | `false` | Master kill switch |
| `--anthropic-api-key` | `ANTHROPIC_API_KEY` | `""` | Shared Anthropic key; takes precedence |
| `--openai-api-key` | `OPENAI_API_KEY` | `""` | Shared OpenAI key |
| `--openai-base-url` | `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Responses API base URL |
| `--ai-recommendations-model` | `AI_RECOMMENDATIONS_MODEL` | (provider default) | Legacy single-model fallback |
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

The configured AI provider receives title, year, and rating signal for the user's most
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
2. Set a provider API key and flip the flag on stage; smoke test
   end-to-end **including the SSE streaming path through any HTTP proxy
   in front of you** (the buffering invariants are easy to break).
3. Monitor Loki logs on `feature=ai_rec`: token usage, resolver drop
   rate, provider error rate, `cache_read` / `cache_write` ratios on the
   recommend path (the latter is how you confirm prompt caching is
   actually working).
4. Flip on prod once stage metrics look sane.
5. Watch daily billing for the selected provider for the first week; if it trends high,
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
- **Full HTTP/SSE pipeline coverage.** Local HTTP fixtures cover both SDK adapters,
  incremental deltas, final usage, malformed/truncated streams, API errors and
  cancellation. OpenAI consumer tests cover recommendation/chip parsing and
  enrichment candidates. The complete handler → `RecommendStream` → concurrent
  resolver path still needs an end-to-end test.
- **Shared cold-start recommendation cache.** A user with empty history
  *and* empty watchlist who taps a default chip produces a fully
  deterministic request: the six English queries in `default_chips.go`,
  the locale, and nothing else — except the `Day / time` line that
  `userPromptForRecommend` injects, which fragments an otherwise identical
  prompt 28 ways. Dropping that line when `HistorySize == 0` would make
  the result cacheable in Redis under `(query_hash, locale, model)`, which
  is what `RecsTTLSeconds` was reserved for. Serving one identical list to
  everyone is the obvious downside; generating a pool of ~30 and returning
  a rotating subset keeps a single cache entry while varying the output.
  Deliberately **not** built yet — at the traffic in Observability & cost
  the saving is small, and `history_size` / `watchlist_size` were added to
  the completion log first so the cold-start share is measured rather than
  assumed.
