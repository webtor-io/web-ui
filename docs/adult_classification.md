# Adult content classification & blur pipeline

Every torrent landing in web-ui gets a per-resource classification row (`resource_metadata`) that records whether it parses as adult, sport, etc. The flag drives:

- **Server-side Gaussian blur** on the poster endpoint for adult resources
- **18+ overlay-badge** on cards in library, continue-watching, and the resource page
- **Adult footnote** under stream/download buttons (replaces the generic 16+ ageGate for anonymous users, adds on top for logged-in users)
- **Auto-purge** of resources rejected as CSAM (stoplist) or rights-restricted from `media_info` and every downstream table

Two opt-out paths exist for users who don't want the blur:
- **Global preference** (`user_settings.show_adult`) → server emits `/lib/poster/raw/...` URLs directly
- **Per-card click-to-reveal** → JS swaps to the `/raw/` URL in place, persists the resource_id in localStorage

## Tables

### `resource_metadata` (migration 57)

```
resource_id   text     PK, FK→media_info(resource_id) ON DELETE CASCADE
is_adult      boolean  NOT NULL DEFAULT false
is_sport      boolean  NOT NULL DEFAULT false
metadata      jsonb    full ptn.TorrentInfo snapshot (representative item)
created_at    timestamptz
updated_at    timestamptz
```

Partial indexes:
- `resource_metadata_is_adult_idx ... WHERE is_adult = true`
- `resource_metadata_is_sport_idx ... WHERE is_sport = true`

Sport is recorded but NOT blurred — broadcasts are legitimate content people would reasonably share. Stored for future filters.

`metadata` is the full ptn.TorrentInfo (resolution, codec, audio, quality, season/episode) of the largest video item. Stored open-shaped via jsonb so future card displays / library filters can read fields without further migrations.

### `user_settings` (migration 58)

```
user_id      uuid     PK, FK→user(user_id) ON DELETE CASCADE
show_adult   boolean  NOT NULL DEFAULT false
created_at   timestamptz
updated_at   timestamptz
```

Typed columns, not JSONB — the v1 setting set is small and we may want filter-by-flag queries (e.g. "how many users opted in"). Switch to JSONB if we ever start accumulating long-tail boolean flags that nothing queries.

Missing row reads as "everything default" (ShowAdult=false). Models load nil-safely.

## Population

`enrichMediaInfo` calls `saveResourceMetadata` after parsing items for media-type detection (line ~498 of services/enrich/enrich.go). The aggregate is "any-true wins":

```
is_adult = OR over all torrentInfos + samples (any ti.Adult)
is_sport = OR over all torrentInfos + samples (any ti.Sport)
metadata = the largest-by-size video item's TorrentInfo (representative)
```

Both `torrentInfos` AND `samples` are scanned — adult releases often ship with a clean main file under a studio-named root folder, and the parser trips on the folder name regardless of which file is "the main feature".

`models.UpsertResourceMetadata` is `INSERT ... ON CONFLICT (resource_id) DO UPDATE`, so re-running enrichment (after a parser change, `--force`, etc.) overwrites the row in place.

## Backfill

`enrich run --metadata-only` is a lightweight backfill that runs ONLY the classification step — no mappers, no AI, no movie/series resolution. Use it after deploying a parser change to re-classify the whole catalog, or after first deploying this feature to populate the table for pre-existing media_info rows.

```bash
# Default: classify resources that don't have a resource_metadata row yet
./server enrich run --metadata-only

# Concurrency knob (default 4 — sized for the 2Gi pod limit when
# running alongside ./server serve; 8+ workers OOM'd in prod). Bump
# higher for local runs where memory is unconstrained.
./server enrich run --metadata-only --workers 4

# Force re-classification of every media_info row (use after a
# parse_torrent_name change, e.g. new adult-studio prefix).
./server enrich run --metadata-only --force

# Target a single hash
./server enrich run --metadata-only --id <hash>
```

The backfill is idempotent — interrupted runs resume from where they left off because `GetResourceIDsWithoutResourceMetadata` returns hashes that don't yet have a row.

Running in-cluster:
```bash
kubectl exec -n webtor deployment/web-ui -- sh -c \
  "nohup ./server enrich run --metadata-only > /tmp/backfill.log 2>&1 & echo PID=\$!"
```

`nohup` is essential — without it the process dies with the kubectl exec session when the bash-tool 10-minute timeout fires. Track progress via:
```bash
kubectl exec -n webtor deployment/web-ui -- grep 'metadata-only backfill: progress' /tmp/backfill.log | tail
# or directly against the DB:
SELECT COUNT(*) FROM resource_metadata;
```

## Stoplist auto-purge

When `retrieveTorrentItems` returns a "permanent rejection" error during classification, `EnsureResourceMetadata` runs the same purge as the `resource.banned` NATS broadcast handler:

```
"found in stoplist"             → CSAM / abuse-store stoplist rule
"restricted by the rightholder" → DMCA / rights takedown
```

Both trigger `models.PurgeResourceByID` which:
1. Refunds funded vault pledges via `vault.tx_log` (OpTypeAbuseRefund=4)
2. Drops `vault.pledge` then `vault.resource`
3. `DELETE FROM media_info` (CASCADE clears movie, series, episode, resource_metadata)
4. Explicit deletes on library, watch_history, cache_index, torrent_resource, user_subtitle, ai_enrich.query

The cleanup SQL lives in `models/purge_resource.go` — single source of truth shared by the NATS handler (`handlers/event/banned.go`) and the backfill. Rationale for treating rightholder takedowns as permanent: historically they aren't lifted often enough to keep dead `media_info` rows around "just in case", and the bookkeeping cost of dead rows is permanent.

Other retrieval failures (DeadlineExceeded, network errors) stay retryable — logged and skipped, picked up on the next backfill run.

## Poster pipeline

Single unified endpoint: `/lib/poster/<resource_id>/<file>` where `<file>` is `<width>.jpg` for card-sized resizes or `og.jpg` for the 1200×630 share canvas. The auth-gated variant `/lib/poster/raw/<resource_id>/<file>` skips the adult blur for opted-in users.

### Source resolution (services/poster_resolver/resolver.go)

```
1. movie_metadata.poster_url       (IMDb poster, by resource_id)
2. series_metadata.poster_url
3. thumbnail row                   (generated at stream/download time)
4. nil → 404 (resize) or brand banner (og)
```

### Blur (services/poster_resolver/render.go)

For resources with `is_adult=true`, the resolver flips `Mode.Blur=true` at miss time. The renderer then applies:

```
sigma   = max(min(width, height) * 0.05, 5)   // pixel-scale-aware
darken  = -10                                 // brightness adjustment
quality = 60                                  // JPEG q (vs 85 unblurred)
```

Sigma scales with image dimension so a 240px card and a 1200×630 OG canvas look equally censored — a fixed sigma would over-blur thumbnails and under-blur the OG canvas. Quality drops to 60 because heavy Gaussian kills high-frequency content anyway, halving payload (~5–10 KB per card, ~20–30 KB per OG canvas).

`Mode.Raw=true` (set by the `/raw/` route) skips the blur entirely. Sport is recorded but never blurs.

### Cache keys (services/poster_resolver/render.go cacheKey)

```
poster/<source-cache-id>/<width>.jpg          // default
poster/<source-cache-id>/blur-<width>.jpg     // adult
poster/<source-cache-id>/raw-<width>.jpg      // /raw/ route
poster/<source-cache-id>/og.jpg               // OG canvas
poster/<source-cache-id>/blur-og.jpg          // OG + adult
```

where `<source-cache-id>` is:
- `imdb_movie-<videoID>` or `imdb_series-<videoID>` (IMDb poster)
- `thumb-<sha1>` (generated thumbnail)
- `default` (brand banner)

This means flipping `is_adult` for a resource doesn't invalidate the unblurred cache, and the `/raw/` and default variants live as independent S3 objects. Two resources matched to the same IMDb work also share the same cached object on the S3 layer.

### Two-tier cache

- **lazymap** (in-process, 5-min TTL): keyed by `<resource_id>/<file>`. Concurrent-request collapser and hot-path skip for the same resource view from the same pod.
- **S3** (persistent): keyed by `<source-cache-id>/<mode>`. Cross-resource sharing (two resources resolving to the same IMDb poster share the same S3 object) and cross-pod persistence.

## URL selection (server-side)

`web.PosterURL(resourceID, width, isAdult, ctx)` picks between the default route and `/raw/`. The /raw/ segment is slotted in **only** when both conditions hold:

1. The resource is classified as adult (`isAdult=true`)
2. The request user has `UserSettings.ShowAdult=true`

Non-adult resources never go through `/raw/` — the endpoint returns the byte-identical image (no blur to suppress) at the cost of an extra auth check. Anonymous users always get the default URL.

Three per-feature helpers delegate to `web.PosterURL`:
- `getCachedPoster240(VideoContentWithMetadata, *web.Context)` — library card grid
- `getResourcePosterURL(*GetData, width int, *web.Context)` — resource page header
- Continue-watching template calls `posterURL` directly with `.IsAdult` + `$.Ctx`

`web.PosterURL` is a package function so handler-side callers can use it directly; the `Helper.PosterURL` method wraps it for template registration via reflection (`{{ posterURL ... }}`).

### What never goes through `/raw/`

- **OG canvas** (`/lib/poster/<rid>/og.jpg`) — shared to third parties (Telegram, Twitter, Stremio addon clients) that haven't opted in. Always default → server-side blur for adult content. Defence in depth: the JS layout doesn't touch `og.jpg` URLs either, and the `/raw/` endpoint 401s for unauth requests.
- **Stremio addon meta** (`services/stremio/library.go makePoster`) — server emits default URL only, Stremio app fetches unauthenticated.
- **Imdb-keyed legacy poster** (`/lib/:type/poster/:imdb_id/:file`) — used by Watchlist / Discover items that aren't bound to a resource_id. Untouched by this pipeline.

## UI

### 18+ overlay-badge

Rendered server-side via the `showAdultBadge` template helper:

```go
showAdultBadge(isAdult, ctx) = isAdult && !ctx.UserSettings.ShowAdult
```

False for non-adult resources (no badge needed) and for opted-in users (badge would clutter every card once the blur is gone). Templates wrap the overlay in `.w-adult-badge` with `data-resource-id` so the click-to-reveal JS can find it.

Badge appears on:
- `templates/views/resource/get.html` — both header variants (enriched + fallback)
- `templates/partials/continue_watching.html` — ribbon cards
- `templates/partials/library/video_list.html` — library grid cards

### Adult footnote

`templates/partials/file.html` swaps the generic 16+ ageGate for an adult-specific one when `isAdult` is true. Wired through two locale keys:

```
ageGate.adultFootnote       // logged-in users: short "you're 18+" confirmation
ageGate.adultFootnoteAnon   // anonymous: 18+ confirmation + TOS/Privacy links
```

The footnote is independent of the global ShowAdult preference — it's a legal/age-confirmation surface, not a UX clutter concern.

### Click-to-reveal

`assets/src/js/lib/adultReveal.js` is the per-card opt-in for users who haven't flipped the global preference. Tapping the 18+ badge:

1. Stores the resource_id in `localStorage['w-adult-revealed']` (max 500 entries, LRU drop)
2. Swaps the card's `<img>` src to `/lib/poster/raw/<rid>/...`
3. Removes the badge

On every initial render and async-fragment rebind, `adultReveal.apply()` replays the stored reveals — the user sees the unblurred poster from first paint on subsequent visits without re-clicking.

Disabled (skip every entry point) when `window._showAdult=true` (the global preference is on — no per-card UI needed) and silently ineffective for anonymous users (the `/raw/` endpoint 401s, so even if they click, nothing useful happens).

## Profile UI

Section `Preferences` on `/profile`, panel `Adult content` with the toggle `Unblur previews`. POST to `/profile/settings` with `show_adult=true|false`, persisted via `services/user_settings.Service.Set` (lazymap-cached, 5-min TTL).

Loaded into `web.Context.UserSettings` via the user-settings middleware so every template render reads from the gin context without an extra DB hit per handler.

## File map

| Path | Role |
|---|---|
| `migrations/57_*resource_metadata*` | classification + parsed-name cache |
| `migrations/58_*user_settings*` | per-user toggles (currently just show_adult) |
| `models/resource_metadata.go` | CRUD + GetResourceIDsWithoutResourceMetadata (backfill query) |
| `models/user_settings.go` | CRUD, nil-safe Get |
| `models/purge_resource.go` | shared cleanup SQL (used by NATS handler + backfill) |
| `services/enrich/enrich.go` | `saveResourceMetadata`, `EnsureResourceMetadata` |
| `services/poster_resolver/` | source resolution, blur, S3 + lazymap cache |
| `services/user_settings/` | lazymap-cached service + middleware |
| `services/web/helper.go` | `PosterURL`, `ShowAdultBadge` (template-registered) |
| `handlers/library/poster.go` | legacy imdb-keyed poster (Watchlist/Discover) |
| `handlers/library/poster_resource.go` | unified `/lib/poster/...` + auth-gated `/raw/` |
| `handlers/event/banned.go` | NATS `resource.banned` consumer (delegates to PurgeResourceByID) |
| `handlers/profile/handler.go` | profile page + `updateSettings` POST |
| `templates/partials/profile/adult_content.html` | toggle panel |
| `templates/partials/file.html` | adult footnote under stream/download buttons |
| `templates/partials/continue_watching.html` | badge + URL choice |
| `templates/partials/library/video_list.html` | badge + URL choice |
| `templates/views/resource/get.html` | badge + URL choice (both header variants + JSON-LD) |
| `assets/src/js/lib/adultReveal.js` | per-card click-to-reveal + localStorage |

## What we don't do (yet)

- **AI content scan** for adult content with innocent filenames. Current detection is purely parse_torrent_name-driven (studio names, scene-release prefixes, JAV codes). Files like `vacation_2024.mp4` with adult content slip through. Would require Claude-backed scan at enrichment time.
- **Session-only reveals**. localStorage persists across sessions; the user has to clear it manually or wait for LRU eviction. Twitter-style "click every view" is doable via `sessionStorage` if the persist semantics ever annoy users.
- **Per-resource cross-device reveals**. Stored locally per browser. Would need a `user_adult_reveals(user_id, resource_id)` table.
- **Cron-driven re-classification**. Backfill is currently a manual `kubectl exec`. After every parse_torrent_name change someone has to remember to run `enrich run --metadata-only --force`. Could move to a helmfile CronJob.
