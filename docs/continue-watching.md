# Continue Watching

The Continue Watching feature tracks user viewing progress and displays a ribbon on the homepage for quick resume.

## Architecture

### Data Model

`models/watch_history.go` — `WatchHistory` table tracks per-file viewing progress:

| Column | Type | Description |
|--------|------|-------------|
| `user_id` | UUID | User (FK to SuperTokens) |
| `resource_id` | string | Torrent info hash |
| `path` | string | File path within the torrent |
| `position` | float64 | Current playback position in seconds |
| `duration` | float64 | Total duration in seconds |
| `watched` | bool | True when position >= 90% of duration |
| `updated_at` | timestamp | Last position update |

Composite PK: `(user_id, resource_id, path)`.

### Query Logic (`GetRecentlyWatched`)

Two-pass approach:

1. **In-progress episodes** — `DISTINCT ON (resource_id)` where `watched = false AND duration > 0`, ordered by `updated_at DESC`. Gets the most recently updated unwatched file per resource.

2. **Next-episode for series** — For resources where all started episodes are fully watched (`watched = true`) but more episodes exist in the `episode` table:
   - Load all episodes for candidate resources (sorted by season/episode)
   - Load watched paths from `watch_history`
   - Find the **next episode after the last watched one** (not the first unwatched — user may have watched some episodes elsewhere)
   - Create a synthetic `WatchHistory` entry pointing to that episode

Results are merged, sorted by `updated_at DESC`, limited, and enriched with metadata.

### Metadata Enrichment (`enrichWatchHistory`)

Joins with `movie` + `movie_metadata` and `series` + `series_metadata` tables to get:
- `Title` — display name (metadata title preferred over parsed title)
- `PosterURL` — **proxied** through `/lib/{type}/poster/{video_id}/240.jpg` (not raw external URL)
- Uses `video_id` (IMDB ID) and `content_type` (movie/series) from metadata to build proxy URL

When no poster is available, the template renders a gradient fallback.

### Poster saved from Discover

Enrichment needs a TMDB/OMDB key, so on an instance without one a card has no
poster. To fix that, Discover remembers the poster the user was looking at:
when they start a stream (`handleStreamClick`) the page posts
`{resource_id, poster_url}` to `POST /discover/poster`
(`handlers/discover/poster.go`), stored in `resource_poster` (migration 77),
**first writer wins**. `poster_resolver.Resolve` checks that table first, ahead
of the movie/series metadata and the thumbnail, so the card uses the artwork
from the catalog. The image is fetched once and cached in the poster S3 bucket
like any other source (`SourceDiscover`).

The URL comes from the browser and is fetched by the server, so
`ValidateStoredPosterURL` (`services/poster_resolver/stored.go`) accepts only
public http(s) URLs: no credentials, no loopback/private/link-local/CGNAT
addresses, after DNS resolution. It runs when the URL is stored and again before
each fetch. Titles started before this existed have no stored poster until the
user opens them from Discover again. Private torrents still get no poster
(`miss()` checks that before resolving).

### Removing a card (x button)

The x on a card calls `POST /watch/dismiss {resource_id}`, which upserts a row in
`continue_watching_dismissed` (migration 77). `GetRecentlyWatched` drops a
resource whose `dismissed_at` is not older than its latest `watch_history`
`updated_at`, so the title comes back by itself when the user watches it again
and no watch progress is deleted. The filter runs before the ribbon limit.
The click handler is in `assets/src/js/app/index.js`.

### Files

- `models/watch_history.go` — model, queries, enrichment
- `models/episode.go` — episode model (season, episode, path, resource_id)
- `handlers/index/handler.go` — fetches `ContinueWatching` for homepage
- `handlers/resource/post.go` — has `ContinueWatching` field (nil) to prevent template error
- `templates/partials/continue_watching.html` — ribbon template with horizontal scroll
- `handlers/watch_history/handler.go` — REST endpoints for position tracking and `POST /watch/dismiss`
- `models/resource_poster.go`, `services/poster_resolver/stored.go` — poster saved from Discover

### Position Tracking Endpoints

- `GET /watch/position?resource-id=X&path=Y` — fetch saved position
- `PUT /watch/position` — save position (JSON body: `resource_id`, `path`, `position`, `duration`)

## Player Resume Flow

### Resume Prompt

When a saved position exists, the player shows a dialog overlay instead of auto-seeking:
- **"Continue from X:XX"** — triggers seek (user tap = user gesture, needed for iOS)
- **"Start from beginning"** — dismisses dialog, playback continues from 0

This replaces the previous auto-seek approach which broke on iOS due to `video.play()` requiring a user gesture after `video.load()`.

### Implementation (`Player.jsx`)

1. `useWatchHistory` hook fetches position on mount, returns `{ resumePosition, resumeReady, forceSendPosition }`
2. When `resumeReady` and `resumePosition > 0`, `showResumePrompt` is set to `true`
3. While prompt is open:
   - Position tracking is paused (`paused` flag) to prevent overwriting saved position with 0
   - Big play button is hidden
   - Click-to-toggle-play is disabled (via `showResumePromptRef`)
4. On user choice:
   - "Continue" → session seek (or `currentTime` for non-session) + `forceSendPosition`
   - "Start from beginning" → dismiss + `forceSendPosition(0, duration)`

### Position Saving

`useWatchHistory` saves position via:
- **Periodic** — every 15s while playing (debounced, min 5s change)
- **On pause** — immediate save
- **On visibility change** — save when tab goes hidden
- **On beforeunload** — `sendBeacon` for reliability
- **`forceSendPosition`** — bypasses debounce/paused, used after seek

### CSS Notes

- `.wt-resume-prompt` requires `pointer-events: auto` (`.wt-player-overlay` has `pointer-events: none` by default)
- Resume buttons use `.wt-resume-btn` / `.wt-resume-btn--primary` / `.wt-resume-btn--ghost` classes
- Styled as frosted glass (backdrop-filter blur) to match player aesthetic
