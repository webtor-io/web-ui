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
- `assets/src/js/lib/discover/components/EmptyStates.jsx` — loading, error, no-results states
- `assets/src/js/lib/discover/prefs.js` — localStorage persistence for user selections
- `assets/src/js/lib/discover/aiClient.js` — thin fetch wrapper for `/discover/ai/*` endpoints (AI recommendations)
- `assets/src/js/lib/discover/components/ai/` — AI recommendations UI: `AISection.jsx`, `AIChipsRow.jsx`, `AIQueryInput.jsx`, `AIRecsGrid.jsx`, `AIRecCard.jsx`
- `handlers/discover_ai/handler.go` — Go handler for AI recommendations (chips, recommend, refine)
- `services/recommendations/` — Claude-backed recommendation pipeline: prompt, context, resolver, quota, cache
- `assets/src/js/lib/discover/watchlistClient.js` — fetch wrapper for `/discover/watchlist/*` endpoints (add/remove/list)
- `handlers/discover_watchlist/handler.go` — Go handler for the watchlist (GET/POST/DELETE)
- `models/movie_watchlist.go`, `models/series_watchlist.go` — DB models, joined with `*_metadata` for the list view

### Key Components

- **DiscoverApp** — root component using `useReducer(discoverReducer, initialState)`. Manages init, catalog loading, search, modal state, and addon wizard flow.
- **StreamModal** — dialog modal driven by `modal` state from reducer. Views: `loading`, `streams` (with reactive filter chips), `episodes` (season tabs + episode list), `progress` (torrent processing).
- **AddonWizard** — two-step guided wizard for discovering and installing Stremio addons. Step 1: source selection (Official Stremio, Community). Step 2: addon browsing with search, filters, and batch install.
- **discoverReducer** — single reducer handling all state transitions. Actions: `INIT_SUCCESS`, `INIT_ERROR`, `SET_PHASE`, `SELECT_TYPE`, `SELECT_CATALOG`, `CATALOG_LOADING`, `CATALOG_LOADED`, `CATALOG_ERROR`, `SEARCH_START`, `SEARCH_RESULTS`, `SELECT_SEARCH_TYPE`, `EXIT_SEARCH`, `SHOW_MODAL`, `CLOSE_MODAL`, `WATCHLIST_IDS_LOADED`, `WATCHLIST_ITEMS_LOADED`, `WATCHLIST_FILTER_TOGGLE`, `WATCHLIST_ADD`, `WATCHLIST_REMOVE`, `SELECT_WATCHLIST_TYPE`.
- **StremioClient** (`lib/discover/client.js`) — fetches manifests, catalogs, search results, meta, and streams from Stremio addon URLs. Uses LRU cache (max 100 entries) and AbortController with 10s timeout on all fetch calls.
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

## Addon Protocol

Uses the [Stremio addon protocol](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/protocol.md):

- Manifest: `GET {baseUrl}/manifest.json`
- Catalog: `GET {baseUrl}/catalog/{type}/{catalogId}.json` (with optional `/skip={n}`)
- Search: `GET {baseUrl}/catalog/{type}/{catalogId}/search={query}.json`
- Meta: `GET {baseUrl}/meta/{type}/{id}.json`
- Streams: `GET {baseUrl}/stream/{type}/{id}.json`
