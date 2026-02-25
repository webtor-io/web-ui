# Discover

The Discover page (`/discover`) lets users browse and search movies and series from Cinemeta and their configured Stremio addons.

## Architecture

Pure frontend feature — no Go backend changes needed. All catalog and search fetches happen directly from the browser to addon URLs.

The UI is built with **Preact** (lightweight React alternative) using hooks (`useReducer`, `useState`, `useMemo`, `useEffect`, `useCallback`). State is managed via a single reducer for predictable updates. The API client and utility modules remain plain JS.

### Files

- `templates/views/discover/index.html` — page template with static header and `#discover-mount` div
- `handlers/discover/handler.go` — serves the page, passes `AddonUrls` from user profile
- `assets/src/js/app/discover.js` — entry point (mounts Preact app, ribbon fallback for non-discover pages)
- `assets/src/js/lib/discover/client.js` — `StremioClient` (API calls, LRU caching, AbortController support)
- `assets/src/js/lib/discover/lang.js` — `LANG_MAP`, `extractLanguages()` (language detection for stream titles)
- `assets/src/js/lib/discover/stream.js` — `parseStreamName()`, `extractInfoHash()` (stream name parsing)
- `assets/src/js/lib/discover/components/discoverReducer.js` — state reducer, initial state, helper functions
- `assets/src/js/lib/discover/components/DiscoverApp.jsx` — root Preact component with all sub-components
- `assets/src/js/lib/discover/components/StreamModal.jsx` — stream modal, episode picker, stream filters

### Key Components

- **DiscoverApp** — root component using `useReducer(discoverReducer, initialState)`. Manages init, catalog loading, search, and modal state. Contains sub-components: `SearchBar`, `TypeTabs`, `SearchTabs`, `CatalogSelector`, `ItemGrid`, `ItemCard`, `LoadMore`, and empty states.
- **StreamModal** — dialog modal driven by `modal` state from reducer. Three views: `loading`, `streams` (with reactive filter chips), `episodes` (season tabs + episode list).
- **discoverReducer** — single reducer handling all state transitions. Actions: `INIT_SUCCESS`, `INIT_ERROR`, `SET_PHASE`, `SELECT_TYPE`, `SELECT_CATALOG`, `CATALOG_LOADING`, `CATALOG_LOADED`, `CATALOG_ERROR`, `SEARCH_START`, `SEARCH_RESULTS`, `SELECT_SEARCH_TYPE`, `EXIT_SEARCH`, `SHOW_MODAL`, `CLOSE_MODAL`.
- **StremioClient** (`lib/discover/client.js`) — fetches manifests, catalogs, search results, meta, and streams from Stremio addon URLs. Uses LRU cache (max 100 entries) and AbortController with 10s timeout on all fetch calls.

### Build Configuration

- **Preact** with `@babel/preset-react` using automatic JSX runtime (`importSource: "preact"`)
- Webpack processes `.jsx?` files, resolves `.jsx` extensions
- Tailwind purge includes `.jsx` files

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

## Addon Protocol

Uses the [Stremio addon protocol](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/protocol.md):

- Manifest: `GET {baseUrl}/manifest.json`
- Catalog: `GET {baseUrl}/catalog/{type}/{catalogId}.json` (with optional `/skip={n}`)
- Search: `GET {baseUrl}/catalog/{type}/{catalogId}/search={query}.json`
- Meta: `GET {baseUrl}/meta/{type}/{id}.json`
- Streams: `GET {baseUrl}/stream/{type}/{id}.json`
