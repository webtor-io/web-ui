# Discover

The Discover page (`/discover`) lets users browse and search movies and series from Cinemeta and their configured Stremio addons.

## Architecture

Pure frontend feature — no Go backend changes needed. All catalog and search fetches happen directly from the browser to addon URLs.

### Files

- `templates/views/discover/index.html` — page template with search bar, grid, modals
- `assets/src/js/app/discover.js` — all client-side logic (StremioClient, DiscoverState, DiscoverUI, main controller)
- `handlers/discover/handler.go` — serves the page, passes `AddonUrls` from user profile

### Key Classes

- **StremioClient** — fetches manifests, catalogs, search results, meta, and streams from Stremio addon URLs
- **DiscoverState** — holds current UI state (selected type, catalog, items, search query/results)
- **DiscoverUI** — DOM manipulation (rendering tabs, grids, modals, search bar, empty states)

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
- **Partial results** — `Promise.allSettled` means failing addons don't block results from working ones

### Exiting Search

- Press Escape, click the X button, or clear the input to exit search mode
- Returns to catalog browsing with the first type/catalog selected

## Streams & Episodes

- Clicking a **movie** opens a stream modal with streams from all stream-capable addons
- Clicking a **series** first fetches meta from Cinemeta (then falls back to user addons) to show an episode picker grouped by season, then fetches streams for the selected episode
- Streams with an info hash link to `/{infoHash}` for playback via Webtor

## Addon Protocol

Uses the [Stremio addon protocol](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/protocol.md):

- Manifest: `GET {baseUrl}/manifest.json`
- Catalog: `GET {baseUrl}/catalog/{type}/{catalogId}.json` (with optional `/skip={n}`)
- Search: `GET {baseUrl}/catalog/{type}/{catalogId}/search={query}.json`
- Meta: `GET {baseUrl}/meta/{type}/{id}.json`
- Streams: `GET {baseUrl}/stream/{type}/{id}.json`
