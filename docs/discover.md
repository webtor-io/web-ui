# Discover

The Discover page (`/discover`) lets users browse and search movies and series from Cinemeta and their configured Stremio addons. It also hosts the AI-powered recommendation section at the top — see [ai_recommendations.md](./ai_recommendations.md) for the full spec of that feature (Claude-backed chips + free-form query + per-card reasons, opt-in via `AI_RECOMMENDATIONS_ENABLED`).

Auth model: `/discover` requires auth. Anonymous visitors are redirected to `/login?from=discover&return-url=/discover?<RawQuery>` (both lang-prefixed via `i18n.LangPath`) — the handler preserves the original query string so a deep link like `/ru/discover?id=tt12042730&type=movie` round-trips intact through sign-in. The login page renders a contextual info card built from `discover.signInCard.intro` + `discover.signInCard.feature1..4` keys; library and vault use the same pattern. The card descriptor is selected server-side in `handlers/auth/handler.go::loginCardFor`. Client-side, popstate inside the Discover SPA also strips the lang prefix (see `assets/src/js/lib/discover/components/useDiscoverUrl.js`) so back/forward navigation works on /ru/discover.

## Architecture

Pure frontend feature — no Go backend changes needed for browsing. All catalog and search fetches happen directly from the browser to addon URLs. Addon management uses Go backend endpoints.

The UI is built with **Preact** (lightweight React alternative) using hooks (`useReducer`, `useState`, `useMemo`, `useEffect`, `useCallback`). State is managed via a single reducer for predictable updates. The API client and utility modules remain plain JS.

### Files

- `templates/views/discover/index.html` — page template with static header and `#discover-mount` div
- `templates/partials/discover.html` — homepage ribbon with poster cards linking to `/discover`
- `handlers/discover/handler.go` — serves the page, passes `AddonUrls` from user profile
- `handlers/stremio/stremio_addon_url/handler.go` — addon URL management including batch-add endpoint
- `assets/src/js/app/discover.js` — entry point (mounts Preact app)
- `assets/src/js/lib/discover/client.js` — `StremioClient` (API calls, LRU caching, AbortController support)
- `assets/src/js/lib/discover/lang.js` — `LANG_MAP`, `extractLanguages()` (language detection for stream titles)
- `assets/src/js/lib/discover/stream.js` — `parseStreamName()`, `extractInfoHash()` (stream name parsing)
- `assets/src/js/lib/discover/components/discoverReducer.js` — state reducer, initial state, helper functions
- `assets/src/js/lib/discover/components/DiscoverApp.jsx` — root Preact component orchestrating all sub-components
- `assets/src/js/lib/discover/components/StreamModal.jsx` — stream modal, episode picker, stream filters
- `assets/src/js/lib/discover/components/AddonWizard.jsx` — guided addon discovery and installation wizard
- `assets/src/js/lib/discover/components/useDiscoverUrl.js` — URL/history management hook
- `assets/src/js/lib/discover/components/discoverUtils.js` — shared utility functions
- `assets/src/js/lib/discover/components/SearchBar.jsx` — search input component
- `assets/src/js/lib/discover/components/ItemGrid.jsx` — card grid display
- `assets/src/js/lib/discover/components/Tabs.jsx` — type tabs, search tabs, catalog selector
- `assets/src/js/lib/discover/components/EmptyStates.jsx` — loading, error, no-results, **catalog-unavailable** states
- `assets/src/js/lib/discover/components/AddonHealthChip.jsx` — page-level addon health surface (warning chip + per-addon status drawer + retry)
- `assets/src/js/lib/discover/manifestCache.js` — `localStorage` fallback for manifests, used to render disabled catalogs from currently-unreachable addons
- `assets/src/js/lib/discover/addonsApi.js` — fetch wrapper around `/stremio/addon-url/:id/refresh-snapshot` (lazy backfill + profile refresh)
- `migrations/52_add_addon_manifest_snapshot.up.sql` — snapshot columns on `stremio_addon_url`
- `assets/src/js/lib/discover/prefs.js` — localStorage persistence for user selections
- `assets/src/js/lib/discover/aiClient.js` — thin fetch wrapper for `/discover/ai/*` endpoints (AI recommendations)
- `assets/src/js/lib/discover/components/ai/` — AI recommendations UI: `AISection.jsx`, `AIChipsRow.jsx`, `AIQueryInput.jsx`, `AIRecsGrid.jsx`, `AIRecCard.jsx`
- `handlers/discover_ai/handler.go` — Go handler for AI recommendations (chips, recommend, refine)
- `services/recommendations/` — Claude-backed recommendation pipeline: prompt, context, resolver, quota, cache
- `assets/src/js/lib/discover/watchlistClient.js` — fetch wrapper for `/discover/watchlist/*` endpoints (add/remove/list)
- `handlers/discover_watchlist/handler.go` — Go handler for the watchlist (GET/POST/DELETE)
- `models/movie_watchlist.go`, `models/series_watchlist.go` — DB models, joined with `*_metadata` for the list view
- `assets/src/js/lib/discover/localizeClient.js` — fetch wrapper for `POST /discover/localize` (batch localized titles/plots, per-id session cache) + `localizedTitle`/`withLocalized` overlay helpers
- `assets/src/js/lib/discover/http.js` — shared `csrfHeaders()` for the discover JSON POST endpoints
- `handlers/discover/localize.go` — Go handler bridging catalog ids to the enrichment localization pipeline
- `migrations/59_tmdb_info_imdb_id_index.up.sql` — partial index on `tmdb.info(imdb_id)` for batch imdb→tmdb lookups

### Key Components

- **DiscoverApp** — root component using `useReducer(discoverReducer, initialState)`. Manages init, catalog loading, search, modal state, and addon wizard flow.
- **StreamModal** — dialog modal driven by `modal` state from reducer. Views: `loading`, `streams` (with reactive filter chips), `episodes` (season tabs + episode list), `progress` (torrent processing).
- **AddonWizard** — two-step guided wizard for discovering and installing Stremio addons. Step 1: source selection (Official Stremio, Community). Step 2: addon browsing with search, filters, and batch install.
- **discoverReducer** — single reducer handling all state transitions. Actions: `INIT_SUCCESS`, `INIT_ERROR`, `SET_PHASE`, `ADDONS_UPDATED`, `SELECT_TYPE`, `SELECT_CATALOG`, `CATALOG_LOADING`, `CATALOG_LOADED`, `CATALOG_ERROR`, `SEARCH_START`, `SEARCH_RESULTS`, `SELECT_SEARCH_TYPE`, `EXIT_SEARCH`, `SHOW_MODAL`, `CLOSE_MODAL`, `WATCHLIST_IDS_LOADED`, `WATCHLIST_ITEMS_LOADED`, `WATCHLIST_FILTER_TOGGLE`, `WATCHLIST_ADD`, `WATCHLIST_REMOVE`, `SELECT_WATCHLIST_TYPE`, `LOCALIZED_MERGED`.
- **StremioClient** (`lib/discover/client.js`) — fetches manifests, catalogs, search results, meta, and streams from Stremio addon URLs. Uses LRU cache (max 100 entries) and AbortController with 10s timeout on all fetch calls. `fetchAllManifests()` returns full per-addon health (status / source / error) — see [Addon Health](#addon-health).
- **AddonHealthChip** — page-level surface for addon outages: hidden when everything is `ok`, otherwise renders a warning chip with retry + a per-addon drawer. See [Addon Health](#addon-health).
- **useDiscoverUrl** — custom hook managing browser history (`pushState`/`replaceState`) and `popstate` events for back/forward navigation.

### Build Configuration

- **Preact** with `@babel/preset-react` using automatic JSX runtime (`importSource: "preact"`)
- Webpack processes `.jsx?` files, resolves `.jsx` extensions
- Tailwind purge includes `.jsx` files

## Homepage Ribbon

The discover ribbon (`templates/partials/discover.html`) shows a row of static movie poster cards on the homepage. Clicking the ribbon or "See more" navigates to `/discover` via `data-async-target="main"`.

Controlled by `{{ if not .Tool }}` — hidden when the page is loaded as a tool/embed.

## Cinemeta (Default Catalogs)

[Cinemeta](https://v3-cinemeta.strem.io) is the standard Stremio metadata provider. It is **always included by default** — its URL is prepended to the addon list on the client side if not already present. This means:

- Users always see Cinemeta's `movie/top` and `series/top` catalogs even without any custom addons configured
- Cinemeta catalogs appear first in the catalog selector (since it's prepended)
- If the user has added Cinemeta manually in their profile, it won't be duplicated
- Cinemeta is also always used for search and meta/episode fetching (see below)

## Browsing

1. On page load, Cinemeta is prepended to addon URLs (if not already present)
2. All addon manifests (including Cinemeta) are fetched in parallel
3. Catalogs are extracted from manifests and grouped by type (movie, series, etc.)
4. User selects a type tab, then a catalog from the dropdown
5. Items are fetched with pagination (`skip` param), Load More button for next page

## Localized Metadata

Stremio addons (Cinemeta included) return English titles/descriptions regardless of `Accept-Language`. For non-English locales the grid and stream modal overlay translations served by the **server enrichment pipeline** — the same `LocalizableMapper` chain (today: TMDB) and `tmdb.localized` cache used by the resource page, library and AI recommendations.

### Flow

1. After any grid surface loads (catalog page, search results, watchlist items), `DiscoverApp` fire-and-forgets `fetchLocalized([{id, type}])` — same pattern as `fetchUserStatuses`
2. `localizeClient.js` filters to bare IMDB `tt*` ids (episode ids, bare `tmdb*` and custom addon ids are skipped), dedupes against a module-level session cache, and POSTs `/discover/localize` in chunks of ≤100
3. `handlers/discover/localize.go` (auth-gated) reads the language from the i18n middleware (URL prefix), fans out `Enricher.LocalizeByID` with bounded concurrency (8). Response contract — three outcomes per id: `{title, plot}` (localized), explicit `{}` (checked, no translation → client caches the negative verdict), id omitted (pipeline error → client leaves it uncached and retries on a later batch)
4. `Enricher.LocalizeByID` walks the mapper chain via capability interfaces: `LocalizableMapper.Localize` first (3-tier cache: lazymap → `tmdb.localized` → TMDB API); only when that finds nothing, one `DirectMapper.MapByID` call pulls a previously unseen id into `tmdb.info` (find + details + external ids) and Localize retries — known ids never pay the extra resolution queries
5. The response merges into `state.localized` (`LOCALIZED_MERGED`, additive-only); `overlayLocalized()` applies it at render time in the `displayItems` memo — items in state stay untouched, English remains the fallback
6. Modal dispatch sites build titles through `localizedTitle(id, ...fallbacks)` / `withLocalized(item)` from `localizeClient.js` (cardClick, episodes view, post-meta patch, both calendar flows) so the localized-over-`meta.name` precedence lives in one place; deep-links with a cold cache fetch the single id and patch the open modal in place

### Properties

- **English sessions cost zero** — `getLang() === 'en'` short-circuits in the client before any network call, and the handler guards server-side too
- **First sight of an id is the only expensive case** (up to 4 TMDB calls); the result persists in `tmdb.info` + `tmdb.localized`, so popular catalog pages are effectively free for everyone after the first non-English viewer
- **No new localization logic** — the endpoint is a thin bridge; adding e.g. Kinopoisk Russian titles means implementing `LocalizableMapper` on the KPU mapper, zero changes in this path
- **Client type hints are not trusted for identity** — only `tt*` ids are accepted (an IMDB id maps to exactly one TMDB entity; `GetTmdbID` resolves the real movie/tv type from `tmdb.info` or the populated find-result array, the hint is just a preference). Bare `tmdb*` ids are rejected because their movie/tv namespaces overlap numerically and a wrong hint could upsert a wrong-namespace title into `tmdb.info`
- **Transient failures don't stick** — a TMDB timeout during a batch leaves those ids uncached on the client (omitted-from-response contract above), so the next grid load retries them; only an explicit "no translation" verdict is pinned for the session
- Episode names in the episodes view stay English (Cinemeta data) — TMDB episode localization is a possible follow-up via `tmdb_episodes.go`

## Search

### Flow

1. User types in the search bar (debounced 400ms, minimum 2 characters)
2. Search queries are sent in parallel to:
   - **Cinemeta** (`v3-cinemeta.strem.io`) for both `movie/top` and `series/top` — always included even if not in user's addon list
   - User's addons that declare search support (`extraSupported` or `extra` containing "search")
3. Results are merged and deduplicated by item ID
4. Default "All" tab shows all results with type badges (Movie/Series) on each card
5. Per-type tabs show counts: "All (14) · Movie (12) · Series (2)"
6. Switching type tabs filters locally — no re-query

### Key Design Decisions

- **Cinemeta always searched** — ensures baseline movie/series search even without search-capable addons
- **No pagination in search** — Stremio search protocol doesn't support `skip` alongside `search`; Load More is hidden
- **Race condition safety** — `searchGeneration` counter discards stale responses when user types faster than responses arrive
- **8-second timeout** — each search request has an AbortController timeout to prevent slow addons from blocking results
- **Request cancellation** — switching catalog/type or starting a new search aborts in-flight requests via AbortController
- **Partial results** — `Promise.allSettled` means failing addons don't block results from working ones

### Exiting Search

- Press Escape, click the X button, or clear the input to exit search mode
- Returns to catalog browsing with the first type/catalog selected

## Streams & Episodes

- Clicking a **movie** opens a stream modal with streams from all stream-capable addons
- Clicking a **series** first fetches meta from Cinemeta (then falls back to user addons) to show an episode picker grouped by season, then fetches streams for the selected episode
- Streams with an info hash link to `/{infoHash}` for playback via Webtor
- Stream filters (source, label, language) are reactive — `useMemo` recomputes the filtered list on every filter change
- "Back to episodes" navigation available from streams view

## Addon Wizard

The AddonWizard component provides a guided two-step flow for discovering and installing Stremio addons.

### Flow

1. **Step 1 — Source Selection**: User selects addon sources (Official Stremio, Community from stremio-addons.net). "Select all" checkbox available.
2. **Step 2 — Addon Browsing**: Addons are fetched from selected sources, enriched with community vote data from GitHub Issues, deduplicated, and sorted (torrent addons first, then by votes). User can search, filter (torrent-only toggle, 18+ toggle), and select addons for batch installation.

### Data Sources

- **Official Stremio** — `https://api.strem.io/addonscollection.json`
- **Community** — `https://stremio-addons.net/api/addon_catalog/all/stremio-addons.net.json`
- **Popularity votes** — fetched from GitHub Issues at `Stremio-Community/stremio-addons-list`, sorted by thumbs-up reactions

### Deduplication

Addons are deduplicated by normalized manifest ID and normalized name. Official source addons take priority over community duplicates.

### Batch Install Endpoint

`POST /stremio/addon-url/batch-add` — accepts `{ urls: [...] }`, validates each addon URL, saves to user profile. Response: `{ added, skipped, limitReached, limit }`.

Tier-based limits: free tier max 3 addons, paid unlimited.

### Integration with Stream Modal

When opened from the streams modal ("Set up addons" button), the wizard:
- Saves the current modal state and URL params in `pendingStreamRef`
- On completion: re-fetches manifests, reloads streams, restores modal and URL
- On skip/close: restores the previous modal state and URL params

### Third-party Disclaimer

Both wizard steps display a disclaimer: all addon sources and addons are third-party services not affiliated with Webtor.

## Watchlist

The watchlist is the discover-level "save for later" surface. It is intentionally distinct from `library` (which tracks downloaded torrents) and `vault` (long-term hosting pledges) — watchlist entries are intent-only, IMDB-keyed, with no torrent attached.

### Surface

- A heart icon sits in the bottom-right of every IMDB card across catalog grids, search results, AI recommendations, and inside the stream modal. Click toggles add / remove, optimistically updating the UI and showing a success toast.
- A two-button switcher `[Catalog | Watchlist]` in the sticky tab bar (anchored next to the type tabs on the same row) flips between the catalog grid and the saved-list grid. The switcher is hidden during search — search always queries Cinemeta + addon catalogs, never the local list, and starting a search exits watchlist mode.
- Inside the watchlist view, the regular `SearchTabs` component is reused for the `All / Movies / Series` segment with live counts. The `CatalogSelector` is hidden in this mode.
- An empty Watchlist view shows a hint to bookmark cards from any other surface.

### URL state and navigation

The active mode is reflected in the URL so back/forward navigation works:

- `?watchlist=1` — Watchlist view active
- `?watchlist=1&watchlist-type=movie` — Watchlist view filtered to movies/series
- No `watchlist` param — Catalog view (default)

The Preact app pushes a history entry on each switcher click and listens to `popstate` so the user can navigate between catalog → watchlist → modal → catalog with the browser controls. Direct links to `/discover?watchlist=1` open the Watchlist view immediately and lazy-load items on first paint.

The browser's bookmark badge state persists server-side and is rehydrated on every page load via `GET /discover/watchlist/ids` — independently of the URL `watchlist` flag.

### Data model

Two parallel tables, symmetric to `movie_status` / `series_status`:

- `public.movie_watchlist (user_id, video_id, source, created_at)` — PK `(user_id, video_id)`
- `public.series_watchlist (user_id, video_id, source, created_at)` — PK `(user_id, video_id)`

There is no FK on `movie_metadata` / `series_metadata`: a user can bookmark before metadata is cached. The handler triggers `Enricher.LookupByVideoID` + `UpsertXxxMetadata` on insert so the next list call has a full card.

`source` records the entry point (`ai`, `search`, `catalog`, `streamy`) — used as a future signal for the recommendation engine and to measure CTR per surface.

### AI cross-feed

The watchlist is fed into the AI Discover prompt alongside the rated-history block: see `services/recommendations/context.go` (`UserContextBuilder.Build` calls `ListUserWatchlist`) and the "Watchlist as taste signal" section in `docs/ai_recommendations.md`. Claude uses it both as a strong taste hint and as an exclusion list, and the `isAlreadyKnown` post-filter drops any Claude-leaked duplicate (matched against `FilterMovieWatchlistVideoIDs` / `FilterSeriesWatchlistVideoIDs`). A user with no watch history but a populated watchlist now gets Claude-generated chips instead of the static cold-start set.

### Free-tier limit

Combined movie + series watchlist size is capped at **200** for free users (`FreeTierWatchlistLimit` in `handlers/discover_watchlist/handler.go`). Paid users are unlimited. The limit is a soft anti-abuse cap, not a billing lever — it only kicks in on add. Hitting it returns 402 with `{ code: "limit_exceeded", limit }`. The Preact app surfaces a toast and rolls back the optimistic insert.

### HTTP contract

```
GET    /discover/watchlist             → 200 { items, video_ids, limit }
GET    /discover/watchlist/ids         → 200 { video_ids, limit }
POST   /discover/watchlist             → 200 { added }   |   402 { code, limit }
DELETE /discover/watchlist/:type/:vid  → 200 { removed }
```

`items` are returned in Cinemeta-shape (`id, type, name, year, poster, imdbRating`) so the same `ItemGrid` renders both modes without source-of-truth branching. `type` is `movie` or `series`; ordering is newest-first, movies and series merged by `created_at`.

## Analytics (Umami)

Events tracked:

- `discover-catalog-loaded` — catalog fetch completed (`type`, `catalog`)
- `discover-search` — search performed (`query`, `count`)
- `discover-streams-loaded` — streams fetched for item (`type`, `id`, `count`)
- `discover-wizard-opened` — addon wizard opened
- `discover-wizard-installed` — addons installed via wizard (`count`)
- `discover-ribbon-click` — homepage ribbon clicked
- `discover-see-more-click` — "See more" link clicked on homepage
- `watchlist-mode-changed` — Catalog | Watchlist switcher flipped (`mode`: `'catalog'`|`'watchlist'`)
- `watchlist-added` — heart icon clicked on a card not yet in the list (`id`, `source`)
- `watchlist-removed` — heart icon clicked on a card already in the list (`id`, `source`)
- `ai-watchlist-toggled` — heart icon clicked on an AI recommendation card (`id`, `on`)
- `stream-from-watchlist` — a card was clicked while the Watchlist view was active (no params; bare counter so the conversion ratio is `count(stream-from-watchlist) / count(watchlist-added)`)

## Addon Health

When a user's Stremio addon (Torrentio, MediaFusion, …) is down, Discover used to degrade silently: failed manifests were filtered out by `Promise.allSettled` and the user saw the same "no streams" / smaller-catalog list as if their selection genuinely had no content. The fix is a centralised per-addon health model surfaced in three places, backed by a server-side manifest snapshot.

### Server-side snapshot (migration #52)

`stremio_addon_url` carries a snapshot of the manifest fields we want to surface even when the addon is currently unreachable:

```
manifest_id          text         -- manifest.id
name                 text         -- manifest.name (used by chip / profile)
manifest_version     text         -- manifest.version
manifest_resources   jsonb        -- string[] of resource names
manifest_types       jsonb        -- string[] of supported types
manifest_logo        text         -- manifest.logo URL (rendered as profile addon icon)
manifest_fetched_at  timestamptz  -- when the snapshot was last refreshed
```

The logo is stored as a third-party URL (no proxying). The profile UI loads it as `<img src referrerpolicy="no-referrer">` with a first-letter initial behind it as fallback — if the image 404s or the addon's CDN dies, the initial keeps the row visually grounded.

All columns are nullable. Pre-existing rows (added before migration #52) carry NULLs and are picked up by the lazy backfill below.

**Capture sites:**
- `POST /stremio/addon-url/add` and `/batch-add` — `AddonValidator.ValidateAndFetch` parses the manifest once and persists the snapshot together with the URL. Failures inside `batch-add` are tolerated (snapshot left NULL); single-add still fails the request because that's the user's only feedback signal.
- `POST /stremio/addon-url/:id/refresh-snapshot` — server-side re-fetch + persist. Returns the new snapshot in JSON. Used by:
  1. The lazy backfill from the Discover client (when `fetchAllManifests` succeeds for an addon whose snapshot is missing or older than 7 days).
  2. The per-addon "Refresh" button in the profile UI.

The 7-day staleness threshold lives in `assets/src/js/lib/discover/addonsApi.js::isSnapshotStale`. Refresh requests are fire-and-forget — failures don't surface to the user beyond an optional toast in the profile.

### Client bootstrap

`handlers/discover/handler.go` serializes the rich shape into `window._addons`:

```js
{ id, url, name, manifestId, version, resources[], types[], fetchedAt }
```

`assets/src/js/app/discover.js` prepends a synthetic Cinemeta seed (we never persist it server-side because Cinemeta is hardcoded) and hands the seeds to `StremioClient`. The chip and selector get names and capabilities up front; the manifest fetch fills in catalogs and confirms health.

### Status model

`StremioClient.fetchAllManifests()` (`assets/src/js/lib/discover/client.js`) returns one record per addon URL — never silently drops failures:

```js
{
    baseUrl,
    manifest,         // null when addon is unreachable AND no cache hit AND no seed
    status,           // 'ok' | 'unreachable' | 'misconfigured'
    source,           // 'fresh' | 'cache' | 'seed' | null
    error,            // string from the fetch failure (optional)
    lastSuccessAt,    // unix-ms (only when source is cache or seed)
}
```

Failure buckets:

- `unreachable` — network error, timeout, 5xx, malformed JSON. User-facing copy: "not responding". Action: retry.
- `misconfigured` — 401/403/404. User-facing copy: "check the addon URL". Action: open profile.

**Fallback chain** on fetch failure:
1. localStorage cache (`manifestCache.read`) — overwritten on every successful fetch, 30-day sanity cap.
2. Server seed (the snapshot from `window._addons`) — survives across browsers and private windows because it lives in PostgreSQL.
3. Nothing — addon shows as host only.

The classification lives in `client.js::classifyError`.

### Status model

`StremioClient.fetchAllManifests()` (`assets/src/js/lib/discover/client.js`) returns one record per addon URL — never silently drops failures:

```js
{
    baseUrl,
    manifest,         // null when addon is unreachable AND no cache hit
    status,           // 'ok' | 'unreachable' | 'misconfigured'
    source,           // 'fresh' | 'cache' | null
    error,            // string from the fetch failure (optional)
    lastSuccessAt,    // unix-ms (only when source === 'cache')
}
```

Failure buckets:

- `unreachable` — network error, timeout, 5xx, malformed JSON. User-facing copy: "not responding". Action: retry.
- `misconfigured` — 401/403/404. User-facing copy: "check the addon URL". Action: open profile.

The classification lives in `client.js::classifyError`. On any failure we read `manifestCache.read(baseUrl)`; if there's a hit, we keep the cached manifest and tag the entry `source: 'cache'` so the UI can still render the addon's catalogs (disabled).

### Manifest cache (`manifestCache.js`)

`localStorage`-backed, key prefix `stremio.manifest.`. Overwrite-on-success only — a failing fetch never invalidates the stored copy. 30-day sanity cap drops genuinely abandoned entries. `prune(activeBaseUrls)` runs once per `StremioClient` construction to drop entries whose addon URL is no longer in the user's profile, plus anything past the cap. Single-entry size guard at 200 KB to prevent one oversized manifest from blowing the storage quota.

This is a per-browser fallback. The server-side snapshot is the cross-browser one — both work together, with the localStorage hit checked first in the fallback chain.

### Centralised state

`state.addons` in the reducer (`discoverReducer.js`) is a flat array built by `buildAddons(addonStatuses)`:

```js
{ baseUrl, host, name, status, source, capabilities, lastSuccessAt, error }
```

Catalogs built by `buildCatalogs(addonStatuses)` carry `disabled: status !== 'ok'` and `addonStatus`. The same source feeds three UI surfaces:

1. **`AddonHealthChip`** in the Discover sticky header. Hidden when every addon is `ok`. Otherwise renders a yellow warning row ("N of M addons unavailable" or "All addons unavailable") with an inline retry button and an expandable drawer listing every addon with its status, capabilities, and a "Manage addons" link to `/profile`.
2. **`CatalogSelector`** (`Tabs.jsx`) groups options by addon when multiple addons are present. Disabled catalogs sit in an `<optgroup label="Torrentio · unavailable">` with `<option disabled>` and a `⚠` prefix — the user sees their previously-available catalogs, just greyed out, instead of having them silently vanish.
3. **`StreamModal` `StreamContent`** (`StreamModal.jsx`) — the modal carries `failedAddons` from `loadStreams`. Two new branches:
    - `streams.length === 0 && failedAddons.length > 0` → dedicated empty-state with explicit attribution ("Torrentio isn't responding") plus the retry button. Different copy from the generic `discover.noStreams`.
    - `streams.length > 0 && failedAddons.length > 0` → thin yellow banner above the list ("Torrentio didn't respond — list may be incomplete") with retry.

`failedAddons` combines two sources: addons whose stream fetch errored mid-flight, plus addons we couldn't even probe (manifest is unreachable AND its capabilities either include `stream` from cache or are unknown). Without the inferred branch, an addon that was already down at page load wouldn't appear in the modal at all.

### Retry paths

- `retryAddons` (chip retry) — re-fetches manifests, updates `state.addons` and `state.catalogs`. Does **not** drop the page back into the loading view; preserves the user's current type/catalog selection.
- `retryStreams` (modal retry) — `retryAddons` + re-issues `loadStreams` for the currently-open modal. Re-uses URL params to reconstruct the stream id (handles series episodes via `tt:season:episode`).
- `retry` (full-page error retry, pre-existing) — only used when init itself throws.

### Pre-selected disabled catalog

When the URL points at a catalog whose addon is currently down (e.g. `?catalog-base=torrentio.strem.fun&catalog-id=top`), `loadCatalog` short-circuits on `catalog.disabled` and the grid renders `CatalogUnavailable` (`EmptyStates.jsx`) — explicit attribution to the addon, retry button, and a copy that nudges the user to pick another catalog. The chip is still visible and still works.

### What's intentionally NOT done

- **Active health-check** (background ping). Too costly in upstream addon load and in trust assumptions. Health is observed when used, not polled.
- **Differentiation beyond `unreachable` / `misconfigured`**. Granular HTTP-code-specific copy adds maintenance overhead without changing what the user can do.
- **Server-side addon health endpoint**. All current consumers (chip, selector, modal) live in the same Discover SPA and read the same client-side state. Cross-feature signals (profile, dashboard) would justify it later.

### Profile UI (per-addon snapshot)

`templates/partials/profile/addon_urls.html` renders each addon row with the snapshot when present:
- 36×36 logo from `manifest_logo` with a first-letter initial behind it as fallback (visible if the image fails to load or there's no logo URL).
- Addon name (bold) over the URL (muted) when `name` is set; otherwise the URL alone.
- Capability pills derived from `manifest_resources` (e.g. `catalog`, `stream`, `meta`).
- Per-addon "Refresh" icon button → `POST /stremio/addon-url/:id/refresh-snapshot`. The row's logo + name + pills are re-rendered inline from the JSON response. Toast feedback via `profile.addons.refreshed` / `profile.addons.refreshFailed`.

### i18n keys

All copy goes through `t/tf` with translations in `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json`:

- `discover.allAddonsDown`, `discover.addonsPartialDown`, `discover.addonGroupUnavailable`
- `discover.addonReady`, `discover.addonUnreachable`, `discover.addonUnreachableCached`, `discover.addonMisconfigured`
- `discover.manageAddons`, `discover.collapse`, `discover.expand`
- `discover.catalogTemporarilyUnavailable`, `discover.catalogUnavailableBody`, `discover.catalogUnavailableBodyGeneric`
- `discover.streamsFailedTitle`, `discover.streamsFailedOne`, `discover.streamsFailedMany`, `discover.streamsFailedBody`
- `discover.streamsPartialFailureOne`, `discover.streamsPartialFailureMany`
- `profile.addons.refresh`, `profile.addons.refreshed`, `profile.addons.refreshFailed`

## Addon Protocol

Uses the [Stremio addon protocol](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/protocol.md):

- Manifest: `GET {baseUrl}/manifest.json`
- Catalog: `GET {baseUrl}/catalog/{type}/{catalogId}.json` (with optional `/skip={n}`)
- Search: `GET {baseUrl}/catalog/{type}/{catalogId}/search={query}.json`
- Meta: `GET {baseUrl}/meta/{type}/{id}.json`
- Streams: `GET {baseUrl}/stream/{type}/{id}.json`
