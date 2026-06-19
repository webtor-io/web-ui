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
| `GET\|HEAD /resolve/*data` | Playback redirect. The JWT in the path carries `{hash, idx, exp}`; resolves to a backend URL via `LinkResolver` and `302`s to it |
| `GET /stream/:type/*id` | Streams for a movie/episode (the pipeline below) |

The personalised addon URL is a short alias (`/s/<code>`, `services/url_alias`)
that points at `/token/<token>/stremio/`. The alias **must be created with
`proxy=true`** (`handlers/profile/handler.go` `getStremioAddonURL`) so addon
resources are served in place — a `proxy=false` alias `301`-redirects every
resource request, which some clients handle poorly.

## Stream pipeline

`Builder.BuildStreamsService` composes layers (inner → outer):

```
Library + AddonComposite        // user's library + external addons
  → CompositeStream             // parallel fan-out, order preserved
  → DedupStream                 // dedupe by infohash (first wins)
  → PreferredStream             // keep only enabled resolutions …
  → LangFilterStream            // … and the preferred audio language
  → EnrichStream                // attach the /resolve URL + ⚡ cache marker
```

**Library streams are exempt from PreferredStream and LangFilterStream.** They
carry a `webtorio|<resourceID>` bingeGroup (`libraryBingeGroupPrefix`); both
filters skip anything with that prefix, because the user already opted into
those exact torrents by adding them to their Vault. Without the PreferredStream
exemption a 4k library title — or a series episode whose filename carries no
resolution token (→ `"other"`) — silently vanishes from results.

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
