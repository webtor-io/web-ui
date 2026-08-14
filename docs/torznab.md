# Torznab indexers

A user can bind Torznab-speaking search endpoints — Jackett, Prowlarr,
NZBHydra2, or a single tracker's own feed — to their account. Their results
then appear next to Stremio addon results everywhere Webtor serves streams:
the personal Stremio addon (`/stremio/stream/...`) and the Discover stream
modal.

Torznab is deliberately modelled as **another source of the same pipeline**,
not as a parallel feature. Everything downstream of the fetch — dedup by
infohash, resolution preferences, language filter, ⚡ availability marker,
the `/stremio/resolve` playback URL — is shared with addons and needed no
Torznab-specific code.

Routes live in `handlers/torznab_indexer/`, the pipeline adapter in
`services/stremio/torznab_stream.go`, and the glue around the protocol in
`services/torznab/`.

## Where the protocol lives

The Torznab protocol itself is **`github.com/webtor-io/go-jackett`** — our
own library, a Torznab client despite the name. `services/torznab` owns only
what is specific to running that protocol against URLs a user typed in: the
egress policy, the result cap, infohash resolution and the Cinemeta title
lookup.

Supporting arbitrary endpoints needed changes in the library, all
backwards-compatible:

| Change | Why |
|---|---|
| `NewTorznab()` — endpoint mode | The URL is used as given. Only Jackett has the `/api/v2.0/indexers/<id>/results/torznab` layout; Prowlarr, NZBHydra2 and bare tracker feeds do not |
| `Caps(ctx, id)` made public | We persist a capabilities snapshot and pick the query shape from it |
| `WithoutCapsValidation()` | `Fetch` otherwise probes caps before every search; we already hold the snapshot, so that would double the request count |
| `attr` no longer namespace-bound | A feed served in the Newznab flavour lost *every* attribute — seeders and infohash included |
| `Settings.UserAgent` | Trackers throttle by user agent; the library was overwriting the caller's with its own |
| `Settings.Client` no longer mutated | `New` installed its api-key middleware on the caller's `http.Client` — including `http.DefaultClient`. With one shared client per process and a client per user endpoint, the transports stacked and a request could carry another user's key |
| magnet/size/pubDate fallbacks | Real feeds put the magnet in `<link>` or the enclosure, the size only in `enclosure@length`, and the date in any of five layouts |

| in-band `<error>` detected on every response | A wrong API key answers a caps probe with HTTP 200 and an error document, which parsed into an empty-but-plausible capabilities struct — reported as "this indexer supports nothing" |
| the response body no longer appears in errors | Callers surface these errors to their own users; after a redirect the body may come from somewhere the caller never meant to fetch |
| HTTP status checked, body read capped at 8 MiB | A login page or a 404 said "malformed XML"; an endpoint streaming an endless body took the process with it |
| feed root pinned to `<rss>` | Any XML — an HTML error page included — unmarshalled into an empty feed, so "not a feed" was indistinguishable from "no results" |

web-ui pins the resulting pseudo-version; `git log` in go-jackett is the
source of truth for which commits that covers. When changing the library
again, push it first — a `replace` pointing at a local checkout builds here
and fails in CI.

## Why the server queries indexers, not the browser

Discover is otherwise a pure frontend feature (see `docs/discover.md`): the
browser talks to addons directly. Torznab is the one exception, for two
reasons that both have to be false for a client-side call to work:

1. **No CORS.** Jackett has never sent `Access-Control-Allow-Origin`
   ([Jackett#2818](https://github.com/Jackett/Jackett/issues/2818)); Prowlarr
   does not either. A browser fetch is rejected before it starts.
2. **Mixed content.** A self-hosted indexer is almost always plain `http` on
   a LAN address. An `https://webtor.io` page cannot reach it regardless of
   CORS.

So Discover posts to `POST /discover/torznab/streams` and the server does the
fetching (`handlers/discover/torznab.go`).

**The consequence is a real product constraint:** the indexer must be
reachable from Webtor's servers. `http://192.168.1.5:9117` works only on a
self-hosted Webtor with `TORZNAB_ALLOW_PRIVATE_NETWORK=true`. On webtor.io
the user needs a publicly reachable indexer. The add form says so, and the
caps probe fails loudly at add time rather than storing a source that
silently returns nothing.

Watch for split-horizon DNS: a hostname that resolves to a LAN address at
home resolves to the WAN address from the cluster, so "it works in my
browser" says nothing about whether Webtor can reach it. The add form
separates the two failures the user can actually act on —
`torznab.IsUnreachable` maps "we cannot route there" to
`error.indexerUnreachable`, which names the constraint, while a rejected key
keeps the indexer's own message.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/torznab/indexer/add` | session | Add an indexer (form: `url`, `api_key`) |
| POST | `/torznab/indexer/delete/:id` | session | Delete one |
| POST | `/torznab/indexer/update` | session | Enable/disable + reorder (list form) |
| POST | `/torznab/indexer/:id/refresh-caps` | session (JSON) | Re-probe `t=caps` |
| POST | `/discover/torznab/streams` | session (JSON) | Discover's server-side stream fetch |

Free tier is capped at 3 indexers — the same allowance as Stremio addon
URLs (`handlers/torznab_indexer/handler.go` `freeTierLimit`), so users have
one number to remember rather than two.

## Data model

`migrations/64_create_torznab_indexer.up.sql` → `models/torznab_indexer.go`.

| Column | Notes |
|---|---|
| `url` | The feed URL with the API key and `t=` stripped. Other pasted parameters stay and are merged back at request time, minus the ones a query owns |
| `api_key` | Split out of the pasted URL (`torznab.ParseFeedURL`) |
| `priority`, `enabled` | Same ordering/toggle contract as `stremio_addon_url` |
| `name` | `<server title>` from the caps probe, falling back to the host |
| `caps`, `caps_fetched_at` | Snapshot of the search modes and their params |

**Why the API key is a separate column** even though Torznab passes it as a
query parameter: the feed URL is rendered back into the profile page, and
splitting the credential out is what keeps it off the page. It is stored in
plaintext, like the secrets already embedded in Stremio addon URLs.

Splitting it out of storage is not enough on its own, because Torznab puts
the key back into the URL on every request and any failed request quotes
that URL in its error. Those errors reach logrus (so Loki) and, on the
refresh endpoint, the browser. `torznab.RedactError` rewrites
`apikey`/`jackett_apikey`/`passkey`-style parameters to `***` while keeping
the original error wrapped, so `IsUnreachable` and `errors.As` still work.
Any new call path that surfaces an indexer error has to go through it.

The GDPR export includes indexers but **not** the key — see
`docs/data_export.md`.

## Query strategy

Torznab has no notion of an IMDb id being authoritative, and support for the
`imdbid` parameter varies per indexer, so `TorznabStream` picks its query
from the caps snapshot and falls back:

1. **Query by id** (`t=movie&imdbid=…`, or `t=tvsearch&imdbid=…&season=&ep=`)
   when caps advertise it, or when caps are missing entirely. Exact, and it
   needs no metadata lookup.
2. **Query by title** (`The Matrix 1999`, `Person of Interest S05E14`) when
   the first attempt is unavailable or returns nothing.

The title comes from Cinemeta (`services/torznab/title.go`), cached 24h. The
fallback query is **built lazily** — resolving titles up front would put a
Cinemeta round-trip on the hot path of every stream request served by an
indexer that answers by id perfectly well.

`imdbid` is sent without the `tt` prefix: the Newznab spec defines it as the
bare number, and while Jackett tolerates both, bare tracker feeds do not.

Queries are narrowed to category 2000 (movies) / 5000 (TV) when the indexer
advertises them, which keeps music and ebook releases out of a movie's
stream list.

**Season/episode go as parameters, not as text.** `t=tvsearch&q=<title>&season=1&ep=5`,
never `q="<title> S01E05"`. The difference is not cosmetic: measured against a
live Jackett/rutracker, the structured form returned 8 results for an episode
where the text form returned 0 — a tracker that names releases "Сезон 1"
matches nothing on an Anglo per-episode string. The text form survives only
as the fallback for indexers whose caps advertise no `season`/`ep`.

Note that rutracker's caps advertise `q,season,ep` and **no** `imdbid`, for
both movie and tv search — which makes the title query the normal path for a
whole class of indexers, not an exception.

**Season results are filtered again on our side** (`matchesRequestedSeason`).
Advertising `season` in caps does not mean honouring it: several Jackett
definitions degrade to a plain keyword search, and the user then gets season
one of a series they opened at season three. A title naming a different
season is dropped; one naming a range that covers the request, or naming no
season at all, is kept — a parsing miss must not cost a good result.

## From result to StreamItem

Two things have to survive the mapping because downstream layers parse them
out of specific fields:

- `Name` = `<indexer> · <tracker>\n<resolution>` — `PreferredStream` parses
  the resolution token out of `Name`, and Discover renders the extra lines as
  chips.
- `Title` = `<release name>\n👤 seeders 💾 size ⚙️ tracker` — `LangFilterStream`
  extracts languages from `Title`, so the untouched release name has to be
  its first line.

### The file index is deliberately absent

Every other stream source names a file: Library streams persist `file_idx`
(migration 60) and Stremio addons send `fileIdx`. A Torznab result names a
*torrent*. Leaving `FileIdx` at its zero value would silently mean "play the
first file", which in a real release is as likely to be a sample, an `.nfo`
or episode 1 of a season pack as the thing the user picked.

So `StreamItem.FileIdxUnknown` marks those streams; `EnrichStream` then omits
the `idx` claim from the playback JWT and skips the availability check (a
cache lookup keyed on file 0 would describe the wrong file), and
`/stremio/resolve` calls `LinkResolver.PickPrimaryFileIdx` at click time,
which lists the torrent and takes the largest video file. The work happens
once, for the stream the user actually opened, rather than for all 30 results
of a search.

For an episode, the token also carries the season and episode numbers, and
`PickEpisodeFileIdx` matches them against the file names inside the torrent
(`S01E05` with any separator, or `1x05`) before falling back to the largest
video. Indexers answer an episode query with season packs at least as often
as with single episodes — on RU trackers that is the normal shape — so
without this every episode of a pack would play episode one.

### Infohash resolution

A stream is only playable if we can address it by infohash, so results are
resolved in cheapest-first order (`services/torznab/infohash.go`):

1. the `infohash` attribute (hex or base32),
2. a `magneturl` attribute or a `magnet:` link,
3. downloading the `link` — http(s) redirects are followed manually, up to 5
   hops; a `magnet:` `Location` is read rather than followed (no transport
   can dial it) and anything else is rejected. Private trackers serve the
   `.torrent` itself, which is parsed with `metainfo.Load`.

Results that survive none of these are dropped rather than shown as dead
rows. Step 3 is the expensive one and is budgeted twice: at most 8 downloads
per query (`maxHashDownloads`) and at most 8 in flight. Steps 1 and 2 cost
nothing and are never counted against it — without the budget a feed that
omits both fields turns one stream request into 30 fetches of up to 8 MiB
each, at addresses the feed itself chooses.

## Timeouts

`CompositeStream` gives each source 5s. That is far too short for Jackett,
which fans out to every configured tracker before answering, so
`StreamsService` gained an optional `TimeoutedService` extension
(`services/stremio/interfaces.go`): a service may ask for its own budget, and
`CompositeStream.GetTimeout` reports the max of its children so nesting a
composite inside a composite does not clamp it back to the default.

Torznab defaults to 12s (`TORZNAB_TIMEOUT`).

## Ordering against addons

`Builder.BuildStreamsService` appends the Torznab composite **after** the
addon composite. `DedupStream` keeps the first stream per infohash, and an
addon result carries a file index that an indexer has no way of knowing — so
when both sources return the same torrent, the addon's copy wins.

Discover merges in the same order for the same reason, but marks the
survivor with the other sources that had it (`+ My Jackett`). Whether your
own indexer found a release is not visible from the winning row otherwise,
and that is precisely what the list gets read for. The Stremio addon does
**not** do this: its stream rows are already dense, and the addon-vs-addon
dedup has always been silent there.

The `discover_only` Stremio setting gates indexers exactly like addons: with
it on, only the user's library reaches Stremio.

## Config

| Flag | Env var | Default | Purpose |
|---|---|---|---|
| `--torznab-timeout` | `TORZNAB_TIMEOUT` | `12s` | Per-search budget |
| `--torznab-max-results` | `TORZNAB_MAX_RESULTS` | `30` | Results kept per indexer per query, highest seeded first |
| `--torznab-user-agent` | `TORZNAB_USER_AGENT` | `webtor.io` | UA sent to indexers |
| `--torznab-allow-private-network` | `TORZNAB_ALLOW_PRIVATE_NETWORK` | `false` | Allow indexer URLs resolving to private/loopback addresses |

`TORZNAB_ALLOW_PRIVATE_NETWORK` is the self-hosted escape hatch. It is off by
default because the URL is user-supplied: the guard sits in the dialer
(`services/torznab/client.go`), so it also covers a hostname that resolves to
`10.0.0.1`, a redirect into the cluster, and the `169.254.169.254` metadata
endpoint. Turning it on for a shared deployment turns the add form into an
SSRF primitive.

## Tests

- `go-jackett/torznab_test.go` — endpoint-mode URL handling (including that a
  stale `q=` in a pasted URL does not survive), caps caching, the Newznab
  attr spelling, the configurable user agent, and a regression guard that two
  clients sharing one `http.Client` keep their own api keys.
- `services/torznab/redact_test.go` — that a key never reaches an error
  message, with a negative control: reverting the redaction makes the test
  print the key it was asked to hide.
- `services/torznab/client_test.go` — the query translation, the in-band
  `<error>` document that arrives with HTTP 200, the result cap, the user
  agent reaching the wire, and a negative control that the private-network
  guard actually blocks loopback.
- `services/torznab/infohash_test.go` — each resolution source, including
  that an HTML login page is rejected instead of hashed.
- `services/torznab/validator_test.go` — caps parsing, API key extraction.
- `services/stremio/torznab_stream_test.go` — query selection per caps
  shape, the empty-result fallback, and that a library id never reaches an
  indexer.
- `services/stremio/composite_stream_test.go` — that a service's own timeout
  is honoured and that nested composites report the max.
