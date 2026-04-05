# User Video Status (watched / rated)

Per-user "watched" state for movies and series, keyed on **IMDB `video_id`** (not torrent `resource_id`). This feed is the foundation for the future rating system and AI recommendations.

## Why IMDB-keyed, not torrent-keyed

`watch_history` (see [continue-watching.md](continue-watching.md)) is torrent-scoped — it tracks playback position per `(user_id, resource_id, path)`. That is the right grain for the resume feature: you cannot resume a specific timestamp across different file offsets.

"Did the user watch this film?" is a different question. It should survive:
- re-downloading the same film in a better quality,
- watching a different dub/release of the same series,
- removal and re-add of the torrent from the library.

To make that work, user watch state lives in a separate set of tables, keyed on `video_id` (the IMDB tt-id stored in `movie_metadata` / `series_metadata` / `episode_metadata`).

## Schema

Three tables, mirroring the existing `movie_metadata` / `series_metadata` / `episode_metadata` split. Migration: `44_create_user_video_status.up.sql`.

### `movie_status`

| Column | Type | Notes |
|---|---|---|
| `user_id` | `uuid` | PK part. FK → `user(user_id) ON DELETE CASCADE`. |
| `video_id` | `text` | PK part. IMDB tt-id from `movie_metadata.video_id`. |
| `watched` | `boolean` | Whether the user declared this movie watched. Phase 1 always writes `true` on mark; unmark deletes the row. The column exists (rather than the row merely "existing") to leave room for future rating-without-watched semantics. |
| `rating` | `smallint` | Future like/dislike: `-1`, `1`, or `NULL`. Reserved column. |
| `source` | `smallint` | How the row was created, stored as a compact numeric enum (`models.UserVideoSource`): `1 = manual` (user clicked the button), `2 = auto_90pct` (user crossed the 90% playback threshold). Migration 44 created the column as `text`; migration 45 converted it to `smallint` and froze the numeric mapping. |
| `watched_at` | `timestamptz` | When the row was set to watched — used for time-weighted signals in the future rec engine. |
| `created_at`, `updated_at` | `timestamptz` | Audit. `updated_at` is maintained by the shared `update_updated_at()` trigger. |

Index: `(user_id, updated_at DESC)` for per-user feed queries.

### `series_status`

Same columns as `movie_status`. Used for:

- **Manual series-level mark** (`source = UserVideoSourceManual`, stored as `1`) — user declared the whole series watched without touching episodes.
- **Auto series-level mark** (`source = UserVideoSourceAutoAllEpisodes`, stored as `3`) — service inserted this row after the user watched every known episode of the series (see rule below).

### `episode_status`

| Column | Type | Notes |
|---|---|---|
| `user_id`, `video_id`, `season`, `episode` | PK | `video_id` is the **series** IMDB id. `(season, episode)` keys into `episode_metadata`. |
| `watched`, `rating`, `source`, `watched_at`, `created_at`, `updated_at` | | Same semantics as `movie_status`. `source` is `UserVideoSourceManual` (1) or `UserVideoSourceAuto90pct` (2). |

Indexes: `(user_id, updated_at DESC)`, `(user_id, video_id)` — the second one drives the series completion count query.

### In-migration backfill

The up-migration replays existing `watch_history.watched = true` rows into the new tables in three steps:

1. Movies: `JOIN movie + movie_metadata`, take `video_id`.
2. Episodes: `JOIN episode + series + series_metadata` with matching `(resource_id, path)`.
3. Series-level: `GROUP BY (user_id, video_id) HAVING count(ues) = count(episode_metadata)` — insert an `UserVideoSourceAutoAllEpisodes` row where the user has watched every known episode.

Migration 44 wrote these rows with textual `source` values (`'manual'`, `'auto_90pct'`, `'auto_all_episodes'`); migration 45 subsequently altered the column to `smallint` and rewrote those three values as `1 / 2 / 3`.

Rows whose enrichment never resolved a `video_id` are silently dropped. They remain valid in `watch_history` but are not part of the IMDB-keyed profile.

## The "all episodes watched" rule

A series is auto-marked watched **only when every episode in `episode_metadata` for that `video_id` has a corresponding `episode_status` row with `watched = true`**.

**Why not "watched the last episode"?**

- **Ongoing series** — the "last" episode today may not be the last tomorrow. If we marked the series on finale watch, a later season would leave the flag inconsistent.
- **Skip-watchers** — people sometimes jump to a finale (procedurals, sports). Marking the whole series based on one episode poisons the future recommendation profile.
- **Symmetry with unmark** — `UnmarkEpisode` cleanly drops the `UserVideoSourceAutoAllEpisodes` row when the condition stops holding. Last-episode logic has no equivalent.

The service computes this after every episode upsert:

```go
total := CountEpisodeMetadataByVideoID(videoID)
done  := CountWatchedEpisodes(userID, videoID)
if total > 0 && done >= total {
    // upsert series_status with source = models.UserVideoSourceAutoAllEpisodes
}
```

For ongoing series that gain new episodes after re-enrichment, the total changes and `done < total` again — the series simply stops being "watched" until the user catches up. This is correct behaviour; we intentionally do **not** preserve stale auto rows.

## Independent series and episode rows

Series-level and per-episode rows are deliberately **decoupled**:

- `MarkSeriesWatched` only writes the series-level row. It does **not** cascade into episode rows. Reason: if the user later unmarks the series, cascading would destroy real per-episode history.
- `UnmarkSeries` only deletes the series-level row; episode rows survive.
- `UnmarkEpisode` deletes the episode row and, if a series-level row exists with `source = UserVideoSourceAutoAllEpisodes`, drops it too. A **manual** series row (`source = UserVideoSourceManual`) is preserved — it represents explicit user intent.

**Display rule**: the resource page and file list treat a series-level `'watched'` row as implying every episode is watched. When no series-level row exists, each episode is displayed by its own row.

## Cross-torrent propagation

Because status is keyed on `video_id`, a user who watched S01E01 on torrent A (Russian dub) will see that episode marked when they open torrent B (English dub) of the same show — provided enrichment resolved both torrents to the same `video_id` in `series_metadata`.

Playback **progress** (the resume bar) remains per-torrent/per-path via `watch_history` — file offsets differ between releases, so carrying a seek position across torrents would be wrong. Only the binary "watched" flag propagates.

Implemented in `handlers/resource/get.go` `prepareGetData`: `WatchedPaths` is augmented by loading `GetEpisodeStatusMapForSeries(videoID)` and setting `WatchedPaths[episode.path] = true` for every episode whose `(season, episode)` is marked in `episode_status`. The file list template (`templates/partials/list.html`) renders the existing green checkmark against the augmented map — no template change required.

## Auto-mark from playback (90% rule)

`handlers/watch_history/handler.go` `updatePosition` calls `models.UpsertWatchPosition`, which now returns a `transitioned bool` — `true` iff the `watched` flag flipped from `false` to `true` on this upsert. When that happens, the handler:

1. Resolves the `(resource_id, path)` to a `VideoRef` (movie or episode) via `models.ResolveVideoFromResourcePath`.
2. Calls `Service.MarkMovieWatched` or `Service.MarkEpisodeWatched` with `source = models.UserVideoSourceAuto90pct`.

If the resource has not been enriched yet (no `video_id`), the resolver returns `nil` and the call is silently skipped. The IMDB-level profile update is **best-effort**: failures are logged but do not fail the playback position request. Resume is the critical path.

Subsequent position frames at 90%+ do not retrigger — the transition check prevents write amplification.

## HTTP endpoints

All under `/library`, protected by `auth.HasAuth` middleware.

| Method | Path | Action |
|---|---|---|
| `POST` | `/library/movie/:video_id/mark` | MarkMovieWatched (source=manual) |
| `POST` | `/library/movie/:video_id/unmark` | UnmarkMovie |
| `POST` | `/library/series/:video_id/mark` | MarkSeriesWatched (source=manual) |
| `POST` | `/library/series/:video_id/unmark` | UnmarkSeries |
| `POST` | `/library/series/:video_id/episode/:season/:episode/mark` | MarkEpisodeWatched (source=manual) |
| `POST` | `/library/series/:video_id/episode/:season/:episode/unmark` | UnmarkEpisode (drops auto series row, preserves manual) |

Form-based, redirect via `X-Return-Url` for progressive-enhancement compatibility with `data-async-target`.

## UI surfaces

- **Resource page** (`templates/views/resource/get.html`) — mark/unmark button rendered inside the header action row, only when the resource has a resolved `video_id` (enrichment complete). Partials: `templates/partials/user_video_status/{movie,series}_button.html`.
- **File browser inline toggle** (`templates/partials/list.html`) — every file row in the resource page file browser has a mark/unmark button next to its size when the path corresponds to an enriched movie or episode. Files that are not enriched (subtitles, samples, NFOs, directories) have no button. Clicking posts to the per-file mark/unmark endpoint and async-reloads the `#list` element. The handler pre-builds `d.PathActions map[path]*PathAction` (see `handlers/resource/path_actions.go`) so the template looks up ready-to-use URLs by path without knowing anything about video_id / season / episode resolution.
- **Library cards** (`templates/partials/library/video_list.html`) — "Watched" badge overlaid on the poster corner. Driven by the transient `Movie.UserWatched` / `Series.UserWatched` field, populated in `handlers/library/index.go` via one bulk query per list using `GetMovieStatusMap` / `GetSeriesStatusMap` (no N+1).
- **Library filter** (`templates/partials/library/watched_filter.html`) — dropdown `All / Unwatched / Watched` next to the sort dropdown. Wired through `shared.IndexArgs.Watched` and a LEFT JOIN in `GetLibraryMovieList` / `GetLibrarySeriesList`. Shown only for movie/series sections.
- **Continue watching ribbon** (`models/watch_history.go` `GetRecentlyWatched`) — calls `filterOutFullyWatched` after enrichment to remove entries whose corresponding `movie_status` or `series_status` is `'watched'`. Covers both manual and auto-series rows.
- **File list** (`templates/partials/list.html`) — existing green checkmark next to each file path, driven by `WatchedPaths`. Augmented in the resource handler with cross-torrent episode state (see above).

## Files

**Models**
- `models/movie_status.go`, `models/series_status.go`, `models/episode_status.go` — table structs, upsert/get/delete/bulk helpers. The shared `UserVideoSource int16` enum (`Manual = 1`, `Auto90pct = 2`, `AutoAllEpisodes = 3`) lives in `movie_status.go` and is used as the `Source` field type on all three structs. Numeric values are frozen by migration 45 and must not be renumbered.
- `models/video_ref.go` — `ResolveVideoFromResourcePath`, used by watch_history auto-mark.
- `models/episode_metadata.go` — added `CountEpisodeMetadataByVideoID`.
- `models/watch_history.go` — `UpsertWatchPosition` now returns `(transitioned, error)`; `GetRecentlyWatched` calls `filterOutFullyWatched`; `WatchHistory` struct has transient `VideoID`/`ContentType` populated by enrichment.
- `models/movie.go`, `models/series.go` — transient `UserWatched bool` field for library cards.
- `models/library.go` — `GetLibraryMovieList` / `GetLibrarySeriesList` take a `watchedFilter string`.

**Service**
- `services/user_video_status/service.go` — `Service` with `MarkMovieWatched`, `UnmarkMovie`, `MarkEpisodeWatched` (triggers completion check), `UnmarkEpisode` (drops auto series rows), `MarkSeriesWatched`, `UnmarkSeries`.
- `services/user_video_status/store.go` — `userVideoStatusStore` interface + `pgUserVideoStatusStore` prod impl.
- `services/user_video_status/service_test.go` — unit tests covering auto-series-on-completion, unmark rollback of auto rows, manual row preservation, empty video_id validation.

**Handlers**
- `handlers/user_video_status/handler.go` — six endpoints, two-level pattern (HTTP / business logic split).
- `handlers/watch_history/handler.go` — injected with `*user_video_status.Service`, triggers auto-mark on the 90% transition.
- `handlers/library/index.go` — `annotateWatched` bulk-prefetches watched state for library list items.
- `handlers/resource/get.go` — loads `MovieStatus` / `SeriesStatus`, augments `WatchedPaths` with cross-torrent episode state, builds `PathActions` for the inline file-list toggle.
- `handlers/resource/path_actions.go` — `PathAction` type and `buildPathActions`; maps each enriched movie/episode file path to its mark/unmark endpoint URLs so the template has no resolution logic.

**Templates**
- `templates/partials/user_video_status/movie_button.html`
- `templates/partials/user_video_status/series_button.html`
- `templates/partials/library/watched_filter.html`
- Edits: `templates/views/resource/get.html`, `templates/partials/library/video_list.html`, `templates/views/library/index.html`, `templates/partials/list.html` (inline per-file watched toggle).

**Migration**
- `migrations/44_create_video_status.up.sql` / `.down.sql` — initial tables (source as text).
- `migrations/45_video_status_source_smallint.up.sql` / `.down.sql` — convert the `source` column of all three tables from `text` to `smallint` and rewrite the existing `'manual' / 'auto_90pct' / 'auto_all_episodes'` values as `1 / 2 / 3` via `ALTER COLUMN ... TYPE smallint USING CASE ... END`.

## Future work (not in Phase 1)

- **Rating** — the `rating` column is reserved. Post-watch modal prompting 👍/👎 and using the signal to weight recommendations.
- **`want_to_watch` / `not_interested`** — would be added as additional boolean columns on the same tables (or a small enum if we have many states). Deliberately not pre-baked into the schema: the original `status text` design was scrapped as speculative — add the columns when the feature actually ships.
- **Recommendations** — collaborative filtering over `(user, video, signal)` tuples, time-decayed by `watched_at`. Requires genre/actor/director extraction from TMDB metadata.
- **GDPR export/delete** — exposing the per-user watch profile for export and full deletion is mandatory before marketing the rec engine publicly.
