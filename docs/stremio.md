# Stremio addon

Webtor exposes a personal Stremio addon so a user's library (and any external
Stremio addons they wire in) is streamable inside Stremio. Routes live in
`handlers/stremio/`, business logic in `services/stremio/`.

## Endpoints

All under `/stremio` (`handlers/stremio/handler.go`):

| Route | Purpose |
|-------|---------|
| `GET /manifest.json` | Addon manifest (`resources: stream, catalog, meta`; `types: movie, series`) |
| `GET /catalog/:type/*id` | The user's library as a Stremio catalog |
| `GET /meta/:type/*id` | Series/movie meta. For series, `videos[]` is built from the library torrent's episodes (`Library.makeVideos`) |
| `GET\|HEAD /resolve/*data` | Playback redirect. The JWT in the path carries `{hash, idx, exp}` (72h TTL — Stremio persists stream URLs across sessions and probes them on next-day resume/binge; 12h made those probes 401); resolves to a backend URL via `LinkResolver` and `302`s to it |
| `GET /stream/:type/*id` | Streams for a movie/episode (the pipeline below) |

`GET /configure` (Stremio's standard config entry): anonymous → login with
`return-url=/stremio/configure`; authenticated → `302 /profile#stremio`. The
anchor rides in the Location header — the only way it survives a login
round-trip — so anonymous-facing links (the tool-page CTA) point here instead
of at `/profile#stremio`.

## Install flow (profile block)

`templates/partials/profile/stremio.html` + `assets/src/js/app/profile/stremio.js`.
Fresh account: one primary action, **Install in Stremio** — the generate form
mints the token and, with JS, the module opens the `stremio://` deep link as
soon as the async island re-renders into the token state. The intent has to
survive the round-trip (the fragment swap is a new subtree), so it rides in
`sessionStorage` (`stremio-install-pending`), set on submit by
`e.submitter`, consumed once on the next render. "Just give me the link" is
the same form without the intent (other device, Stremio Web). Without JS the
form posts, the page reloads into the token state and Install is the first
button there. Umami: `stremio-install-addon` with `stage=fresh|token|auto`,
`stremio-generate-addon-url` for link-only, `stremio-download-app`.
`/instructions/stremio` leads with this path and keeps the manual one below.
Review the fresh state on your own account: `/profile?preview=stremio-fresh`.

Token management (both `POST`, auth-gated, rendered by `templates/partials/profile/stremio.html`):

| Route | Purpose |
|-------|---------|
| `POST /url/generate` | Issues the addon token. **Idempotent** — `models.MakeAccessToken` keeps the existing token on conflict, so pressing it twice never breaks an installed addon |
| `POST /url/regenerate` | Rotates the token (`models.RegenerateAccessToken`). **Destructive**: the previous URL stops resolving immediately and the addon must be reinstalled on every device. Gated behind an expanded collapse in the UI |

Generation stays an explicit user action rather than something issued on first
profile view — partly as a signal of intent, and partly because
`access_token` with scope `stremio:read` is the metric for "user connected
Stremio" (see `docs/onboarding_empty_states.md`); auto-issuing would turn it
into "user opened the profile" and destroy the measurement.

The personalised addon URL is a short alias (`/s/<code>`, `services/url_alias`)
that points at `/token/<token>/stremio/`. The alias **must be created with
`proxy=true`** (`handlers/profile/handler.go` `getStremioAddonURL`) so addon
resources are served in place — a `proxy=false` alias `301`-redirects every
resource request, which some clients handle poorly.

> This was `false` in the code until the 2026-08 security pass, and the cost
> was not only client compatibility: the `301` carried the raw access token in
> its `Location`, so the Stremio client stored an account credential and
> replayed it on every request. Because `CreateOrGetURLAlias` matches on the
> URL, aliases minted before the fix keep `proxy=false` until the user
> regenerates the token — flipping the existing rows is a separate data fix.

## Stream pipeline

`Builder.BuildStreamsService` composes layers (inner → outer):

```
Library + AddonComposite + TorznabComposite   // library, addons, indexers
  → CompositeStream             // parallel fan-out, order preserved
  → DedupStream                 // dedupe by infohash (first wins)
  → PreferredStream             // keep only enabled resolutions …
  → LangFilterStream            // … and the preferred audio language
  → EnrichStream                // attach the /resolve URL + ⚡ cache marker
```

**Torznab indexers are a source, not a feature of their own.** They enter the
pipeline as one more `StreamsService` and every layer below treats them like
addon streams — see [torznab.md](./torznab.md). Two consequences worth
knowing here: the Torznab composite is appended *after* the addon composite
so `DedupStream` prefers the addon copy of a shared infohash (it carries a
file index the indexer cannot know), and indexers are gated by the same
`discover_only` setting as addons.

Per-service timeouts are no longer fixed at 5s: a source may implement
`TimeoutedService` to ask for its own budget (Torznab uses 12s, since a
Jackett query fans out to every configured tracker). `CompositeStream`
reports the max of its children, so nesting one composite inside another
does not clamp the inner budget back to the default.

**Language detection falls back to the script.** `ExtractLanguages` matches
explicit tags first (`rus`, `рус`, `ukr`, …); when a title carries none, a
Cyrillic title counts as Russian, or Ukrainian if it has letters Russian does
not (`і ї є ґ`), and the Russian-scene voice-over markers `AVO`/`MVO`/`DVO`
count as Russian on their own. Found in production: a user with Russian as
their preferred language had a rutracker release dropped from their Stremio
list because the title was transliterated English tagged only "AVO". Keep
`assets/src/js/lib/discover/lang.js` in sync — Discover shows the same chips.

**Library streams are exempt from PreferredStream and LangFilterStream.** They
carry a `webtorio|<resourceID>` bingeGroup (`libraryBingeGroupPrefix`); both
filters skip anything with that prefix, because the user already opted into
those exact torrents by adding them to their Vault. Without the PreferredStream
exemption a 4k library title — or a series episode whose filename carries no
resolution token (→ `"other"`) — silently vanishes from results.

### File index is persisted, not re-derived at /stream time

Each library `StreamItem` needs the torrent **file index** (`FileIdx`) — it
goes into the `/resolve` JWT and the ⚡ availability check, and is the
`content_id` rest-api's `/resource/<hash>/export/<idx>` expects.

`FileIdx` is stored on the `movie` / `episode` rows (`file_idx` column,
migration 60) at enrich time. The value comes straight from rest-api's
`ListItem.Index` — the file's position in the torrent's natural file order
(`r.Files`), authoritative and independent of the sorted/paginated `/list`
order. `Library.getStreamItem` reads it via `resolveFileItem` and derives the
filename from the path basename — **no rest-api call on the `/stream` hot
path**.

This replaced the old behavior where `retrieveTorrentItem` paginated
`/resource/<hash>/list` on every request, counting files until the path
matched. For files deep in large season packs that meant 2–3 sequential
rest-api round trips, and under the `CompositeStream` 5s per-service timeout it
intermittently dropped the **entire** Library result — vault streams silently
missing in Stremio, addon streams left on top. See migration 60 for the
rationale.

`resolveFileItem` falls back to the legacy `retrieveTorrentItem` list-walk when
`file_idx` is `NULL` (rows enriched before the column existed). The fallback is
nil-safe (a path no longer in the listing yields no stream, not a nil-deref)
and returns the matched item's **`ListItem.Index`** — the torrent's natural
file index — not a positional count over the sorted list. The old positional
count resolved the *wrong* file whenever the torrent's natural `r.Files` order
didn't match the folders-first/name sort (e.g. a season pack with scrambled
file order).

**Self-heal.** Adding a resource to a library calls `jobs.Enrich`, but enrich
short-circuits for already-enriched resources (`TryInsertOrLockMediaInfo`
returns nil), so a pre-migration-60 resource would keep `file_idx = NULL` and
fall back to the slow/legacy path forever. `Enricher.backfillFileIndex` closes
that gap: on the skip branch it fills `file_idx`/`file_size` directly from
rest-api `ListItem.Index/Size` (`models.FillFileIndex`), gated by
`HasNullFileIdx` so it issues no rest-api call once populated. A one-time
backfill seeds existing rows; self-heal covers everything added afterward.

> Cross-service note: `ListItem.Index` requires rest-api ≥ the release that
> added it. Bump the `github.com/webtor-io/rest-api` pin in `go.mod` after that
> release; until then enrich stores `file_idx` from a stale index field and the
> Library fast path may resolve the wrong file. Deploy rest-api first.

### Stream presentation (marker + title)

Library streams carry a `⭐` prefix on their `name` badge (added in
`EnrichStream.enrichStream` for any `isLibraryStream`), so the user's own
entries are unmistakable next to addon results.

`Library.makeStreamTitle` builds a Torrentio-style multi-line **`Title`** from
data already on the row — no extra fetch. It MUST go in `Title`, not
`Description`: Stremio (and addons like Torrentio) render `Title` and ignore
`Description` — a library stream that only set `Description` shows up in the
JSON but is invisible in the Stremio app.

```
The Big Bang Theory · S05E14 [2012 BluRay 1080p]
💾 1.41 GB  ⚙️ Library
🇷🇺 / 🇬🇧
```

- line 1 — clean title (+ `S<season>E<episode>` for series) and a
  `[year quality resolution]` tag built from the ptn snapshot (`md`), each part
  optional;
- line 2 — `💾 <size>` from the persisted `file_size` column (`bigint`,
  migration 60, captured from `ListItem.Size` at enrich time) + the `⚙️ Library`
  source. No 👤 seeders line — vault content is cached, not P2P;
- line 3 — language flags via `ExtractLanguages(filename)` (`lang.go`),
  de-duplicated, omitted when nothing is recognised. **Known gap:** ptn rarely
  populates `md.language` (~0.05% of rows) and the filename often carries no
  language token, so most flags are missing. The planned fix is to extract
  languages into `md` at enrich time and read from there — see
  `project_stremio_library_languages_in_md` memo.

## Binge-watching (auto-play next episode) — the non-obvious contract

This is easy to break and hard to diagnose, so read this before touching the
stream or resolve handlers. Mechanics are in `stremio-core`
(`src/models/player.rs`, `src/types/resource/stream.rs`):

1. **Matching is `bingeGroup` string-equality only.** `Stream::is_binge_match`
   compares `behavior_hints.binge_group` of the playing stream to each candidate
   of the next episode; nothing else (not the source type, url, or infohash). So
   the bingeGroup must be **identical across episodes** — webtor keys it by the
   torrent (`webtorio|<resourceID>`), which is stable for a season-pack.
2. **Next-episode streams are pre-loaded eagerly** when the player opens: Stremio
   reuses the playing stream's request and swaps the video id to the next
   episode, then loads `/stream/...:S:E+1`. If that response isn't `Ready` with a
   matching bingeGroup, the next-episode button falls back to source-select.
3. **Stremio validates the chosen stream's playback URL with a `HEAD` request**
   before auto-playing. Our playback URL is `/stremio/resolve/<jwt>`, so
   **`/resolve` must answer `HEAD`** (it mirrors `GET` → `302`). Gin does not
   auto-register `HEAD` for a `GET` route; a `HEAD` 404 makes Stremio treat the
   next episode's stream as dead and bounce to source-select **every time**.
   Guarded by `TestResolveRouteAcceptsHEAD`.

P2P addons (e.g. Torrentio without debrid) play via Stremio's torrent engine and
skip the HTTP HEAD probe, so they binge even when an HTTP addon does not — a
useful tell when debugging: if Torrentio binges and webtor doesn't, suspect the
playback URL (HEAD reachability / non-404), not the bingeGroup.
