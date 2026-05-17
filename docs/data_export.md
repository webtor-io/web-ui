# Data Export (GDPR Article 20)

The `/profile/export` endpoint returns a single JSON document containing every
user-keyed row Webtor stores for the authenticated account. It implements the
GDPR right to data portability: machine-readable, self-contained, downloadable
by the user without any manual review on our side.

- **Route:** `GET /profile/export` (auth-gated via `auth.HasAuth`)
- **Content-Type:** `application/json; charset=utf-8`
- **Disposition:** attachment, filename `webtor-data-export-YYYY-MM-DD.json`
- **Cache-Control:** `no-store` (responses contain credentials)
- **Handler:** `handlers/profile/handler.go` → `Handler.export`
- **Builder:** `services/data_export/export.go` → `data_export.Build`

## Wire format

The top-level object has a `schema_version` integer. Bump
`data_export.SchemaVersion` whenever the JSON shape changes in a way that
breaks consumers.

```jsonc
{
  "schema_version": 1,
  "generated_at": "2026-05-17T12:34:56Z",
  "user": { "user_id": "...", "email": "...", "tier": "...", ... },
  "library": [...],
  "watch_history": [...],
  "movie_statuses": [...],
  "series_statuses": [...],
  "episode_statuses": [...],
  "movie_watchlist": [...],
  "series_watchlist": [...],
  "stremio_addon_urls": [...],
  "stremio_settings": { ... } | omitted,
  "embed_domains": [...],
  "streaming_backends": [...],
  "user_subtitles": [...],
  "access_tokens": [...],
  "vault": { ... } | omitted
}
```

Empty collections are emitted as `[]` (not omitted) so consumers can detect
"feature exists, user has nothing" vs "feature not in this schema version".
Optional sub-objects (`stremio_settings`, `vault`) are omitted entirely when
the user has never used the feature.

## Sources

| JSON field            | Source table / function                                            |
| --------------------- | ------------------------------------------------------------------ |
| `user`                | `models.User` (via `GetUserByID`)                                  |
| `library`             | `models.GetLibraryTorrentsList` (with `TorrentResource` join)      |
| `watch_history`       | `models.ListAllWatchHistory`                                       |
| `movie_statuses`      | `models.ListAllMovieStatuses`                                      |
| `series_statuses`     | `models.ListAllSeriesStatuses`                                     |
| `episode_statuses`    | `models.ListAllEpisodeStatuses`                                    |
| `movie_watchlist`     | `models.ListMovieWatchlistItems` (joined view with title/year)     |
| `series_watchlist`    | `models.ListSeriesWatchlistItems`                                  |
| `stremio_addon_urls`  | `models.GetAllUserStremioAddonUrls`                                |
| `stremio_settings`    | `models.GetUserStremioSettings`                                    |
| `embed_domains`       | `models.GetUserDomains`                                            |
| `streaming_backends`  | `models.GetUserStreamingBackends`                                  |
| `user_subtitles`      | `models.ListAllUserSubtitles`                                      |
| `access_tokens`       | `models.ListUserAccessTokens`                                      |
| `vault.balance`       | `vault.GetUserVP`                                                  |
| `vault.pledges`       | `vault.GetUserPledges`                                             |
| `vault.transactions`  | `vault.ListUserTxLogs`                                             |

## Credentials

The export includes secrets the user supplied or that we issued back to them:

- `streaming_backends[].access_token` — RealDebrid / Torbox API key the user
  pasted into their profile. Already visible on the profile page UI.
- `access_tokens[].token` — Webtor-issued tokens that compose the Stremio
  addon URL and the WebDAV URL the user already sees on the profile page.

The export is delivered over an authenticated session and the user can see
the same values in-app, so the file does not reveal anything the user
doesn't already control.

## What is NOT exported

- `ai_enrich.query` — global title-normalisation cache, not user-keyed.
- AI recommendation quota counters — ephemeral Redis state that rolls over
  daily (`services/recommendations/quota.go`). Not "data we hold about the
  user" in the GDPR sense.
- `speedtest_result` — session-keyed, not user-keyed.
- Shared metadata tables (`movie_metadata`, `series_metadata`,
  `torrent_resource`, …) — public catalog data, not personal. The export
  references them by id so the user can rejoin against any future schema.

## Adding new user-keyed tables

**If you add a new table that contains user-keyed data, you MUST update the
export.** The flow:

1. Add a `ListAllX(ctx, db, userID)` (or equivalent) function in `models/`
   if there isn't one that returns the user's full set.
2. Add a DTO and a JSON field to `services/data_export/export.go`.
3. Populate it in `data_export.Build` (or one of its `fill*` helpers).
4. Bump `data_export.SchemaVersion` if you change an existing field, not for
   pure additions.
5. Update this table and the `5. Data Deletion & Portability` section of
   `templates/views/legal/privacy.html` if the new field is meaningfully
   user-facing (e.g. a new long-term preference, not a derived cache).

Missing a table here = a real GDPR Art. 20 compliance gap. Treat the export
as a hard requirement, not a nice-to-have, for every new user-keyed schema.
