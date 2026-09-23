# User-facing errors

How an `error` becomes the sentence a person reads, and which sentences exist.

## Mechanics

`services/web/user_error.go`:

- `UserError{Key, Err}` — an error that already knows its key (`NewUserError`).
- `ClassifyError(err)` — everything else: a switch over the error, falling back
  to `error.generic`. It opens with `errors.Is` cases for the query errors of
  `common.ResolveQueryHash` (below) — by identity, not text, because the form
  wraps them as `wrong resource provided query=<what was pasted>`, and a pasted
  title containing "Unavailable" or "PermissionDenied" would otherwise match a
  backend class. The rest is a substring switch over `err.Error()`. Order
  matters: the more specific wrappers (`forbidden`, `not_found`,
  `resolution_not_supported`, `status=415`) sit above the broader
  streaming-chain classes, and `failed to parse magnet` sits above
  `wrong resource provided`, which is what wraps it.
- `StatusForErrKey(key)` — HTTP status for the error page; retry-able classes
  (`service_unavailable`, `upstream_unavailable`) answer 503.

Render points: the job progress log (`jobs/jobs.go` errorFormatter), the error
page (`services/web/middleware.go`), redirects with `?err=` (`services/web/helper.go`),
and the 404 home page of a resource URL that names nothing
(`handlers/resource/get.go` `notFound`: `error.invalid_resource` /
`error.not_found`, see `docs/status_and_caching.md`). An unknown legal page
answers 404 with `error.page_not_found` (`handlers/legal`).
Every render logs one structured line — `user error shown` with `err_key` and
`surface=job|page` — so the distribution, and the share still landing in
`error.generic`, can be read off Loki:

```
{app="web-ui"} |= "user error shown" | logfmt | err_key != ""
```

## What the form accepts (2026-09)

The search field on the home page and the 20 tool pages (`POST /`, also the
`/magnet:?…` GET route, the extension's `/ext/magnet` and the embed's `magnet`
setting) goes through `common.ResolveQueryHash`. After trimming it accepts:

| Input | Outcome |
|---|---|
| a magnet URI, any case of `magnet:` | its btih; a 32-character base32 btih is upper-cased first (the library decodes only the upper-case alphabet: on 2026-09-23, 17 valid lower-case magnets were refused as broken, 35 submits) |
| a magnet inside other text (`url=magnet:?…`) | the magnet, up to the first whitespace |
| a magnet that does not parse | tried once more percent-decoded (`magnet:?xt%3Durn:btih:…%26dn%3D…`, a magnet that was itself a query value: `/magnet2torrent?magnet=…`), then the input's standalone 40-hex token (`magnet://?xt=…`, `magnet:? xt=…`, a word joiner after the hash); only then `error.magnet_invalid`. A btmh-only magnet stays `error.v2_hash` |
| the whole input is 40 hex or 32 base32, any case (also as `urn:btih:…`) | that infohash |
| a standalone 40-hex token anywhere (`common.V1HashTokenR`, the share target's rule) | that infohash — a resource-page link with or without a scheme, a .torrent cache that names files by hash, `Info Hash: <hash>`, `<name> <hash>` |

The `url=magnet:?…` shape used to come from our own `/show` redirect (where extension builds up to 0.1.12 send a clicked magnet): it wrapped the magnet in a second `url=`. Since 2026-09-23 `/show` passes the magnet unchanged (`handlers/migration`, test `TestShowMagnetRedirectCarriesTheMagnetUnchanged`); the rule above stays for links already out there.

Everything else is refused, and says what it was:

| Key | Error (`common.`) | Input | Share of the 1 126 submits refused on 2026-09-23, replayed through the new rules |
|---|---|---|---|
| `error.free_text` | `ErrQueryFreeText` ("no infohash found in query", the old wording) | anything that is not a link, a whole hash or a text with a 40-hex token: titles, site names, a name next to a cut hash | 66.5% |
| `error.webpage_url` | `ErrQueryWebPage` | an http(s) URL without a 40-hex token, not to a `.torrent` | 19.5% |
| `error.torrent_url` | `ErrQueryTorrentURL` | an http(s) URL whose path ends in `.torrent` and carries no hash | 2.0% |
| `error.magnet_invalid` | `ErrMagnetInvalid`, `ErrMagnetNoHash` | a magnet that does not parse or has no `xt` (cut short, a line break inside the hash), with no 40-hex token to fall back on | 5.3% |
| `error.hash_length` | `*HashLengthError` (`errors.Is` `ErrHashLength`) | the whole input is hash characters, but not as many as a hash has: 16+ hex with a digit and a letter, or 24–40 base32 in one case with two digits 2–7; the message quotes both numbers ("it has 39 characters, a full one has 40") | 0 (the old rule accepted these and failed in the load job, so they were not in that day's refusals) |
| `error.v2_hash` | `ErrV2Only` | a 64-hex SHA-256 digest (or the 68-hex `1220…` multihash, `urn:btmh:`), a btmh-only magnet | 0 |

The other 6.6% of those refused submits are now accepted: lower-case base32
magnets, magnets pasted after `url=`, a hash with a trailing space, and
(since the fallback of a magnet that does not parse) 26 submits whose hash
was readable: `magnet://?xt=…`, a typo in `urn:btih`, text after the hash.

`error.hash_length` is the one message with numbers. `web.ErrArgsOf(err)`
returns them, `RedirectWithErrorAndPath` puts them next to `?err=` as
`err_count` and `err_full` (and drops stale ones from the return URL),
`web.ErrArgsFromQuery` reads them back for the home and tool pages (only a
count of 1–1000 and a full length of 40 or 32 pass), and the page renders
`tn` with them (plural forms in the locale files). The job log formats it the
same way (`jobs/jobs.go`). A page that shows `ErrKey` with a plain `t` would
print `<no value>` for the count, so a new render point for this key needs
`ErrArgs` too.

Why these rules:

- **Nothing shorter than 40 hex is cut out of a longer string.** The old rule
  took the first run of 5–40 hex anywhere (`[0-9a-f]{5,40}`): "S01E02" became
  `btih:01e02`, a URL with a numeric id became its digits, and ~490 such
  inputs a day (2026-09-23, "encoded length 5…35" in the log) reached the load
  job and ended on the "This magnet link is broken" card. `common.SHA1R` still
  exists as a sanity check of the resource id in `GET /:resource_id`, not as a
  parser.
- **A standalone 40-hex token is taken wherever it stands**, with the share
  target's `\b` guards: a link to a resource page or to a hash-named .torrent,
  `webtor.io/<hash>` without a scheme, `Info Hash: <hash>`, a name next to the
  hash. Production has always accepted these, and `/share` does too; the
  guards are what keep the fragments out — a token cannot be cut from
  "S01E02", a numeric id, a 41-hex run or a 64-hex v2 digest. A token that is
  not an infohash (a file checksum) ends on the dead-magnet card, as it always
  did.
- **A hash of the wrong length is told so.** A 39- or 41-character hash is a
  copying slip, and "Webtor doesn't search by title" answered the wrong
  question. The shape is kept narrow (see `hashLength`): no word is 16 hex
  letters long, a long plain number is not a hash, and a run-together title
  rarely has two of the digits 2–7 and none of 0, 1, 8, 9.
- **A `.torrent` link is not fetched.** The form never did (it only worked when
  the URL carried the hash); the embed does fetch `torrentUrl`, through its
  own restricted client. The message says so and asks for the file.
- **64 hex is refused, not cut.** The old first match took its first 40
  characters, a v1 hash that exists nowhere; v2-only IDs are not supported
  anywhere in the pipeline (btmh-only magnets were already refused), so the
  answer is "use a magnet with a v1 infohash, or the file".

On the tool pages the submit button says `home.open` ("Open") and the field
shows an arrow instead of the loupe; the home page keeps `home.search` for
now. The Umami event stays `search` on both, with `page=<tool url>` on tool
pages. Test tables: `TestResolveQueryHash_FormInputs` (services/common),
`TestClassifyError_FormInput` (services/web), `TestIndexFormSaysOpenOnToolPagesOnly`
(services/template).

## Streaming-chain classes (2026-09)

| Key | Matches | What happened | What the user can do |
|---|---|---|---|
| *(card)* `load/errors/magnet` | `*scripts.MagnetError` from the load job | the magnet did not become a torrent: `dead` (no peer had the metadata within the wait, 60 s by default) or `invalid` (broken link); the load job renders a card instead of a red line, with a countdown while waiting | dead: "try again for 10 minutes" (`magnet-wait=long`; the form targets the log host `#log-load` whose `data-async-layout` re-renders `partials/load/progress` with the new job — same shape as the streaming retries into `#log-<item>`; rest-api and magnet2torrent cap at 10 min), .torrent or another source; invalid: fix the link. Dev: `/magnet:?xt=urn:btih:<40 hex>&debug=magnet_dead\|magnet_dead_long\|magnet_invalid` |
| `error.magnet_no_metadata` | `failed to magnetize`, `magnet timeout` | magnet2torrent found no peer with the metadata within the 60 s deadline; measured 2026-09-04: such magnets stay unresolvable on a warm client too (5/60), i.e. dead magnets | use the .torrent file or another source; retrying rarely helps (504) |
| `error.magnet_invalid` | `common.ErrMagnetInvalid` / `ErrMagnetNoHash`, or the text `failed to parse magnet` from elsewhere | the magnet link itself is broken (infohash missing / cut short) | copy the full link or upload the .torrent (400) |
| `error.upstream_unavailable` | `failed to retrieve resource / stream url / download link`, `stats returned status`, `warmup returned status` | rest-api / thp / seeder did not answer | retry in a minute (ours to fix) |
| `error.probe_failed` | `failed to get probe data` | content-prober could not read the media | download instead |
| `error.resolution_not_supported` | `over 1080p is not supported` | transcoder refuses >1080p non-h264 | download instead |
| `error.transcode_failed` | `transcoder session creation failed status=415` | transcoder refused the source (codec, container) | download instead |
| `error.transcode_unavailable` | any other `transcoder session creation failed` | the converter itself failed to start | retry, or download |
| `error.stream_stalled` | `session buffer timeout exceeded`, playlist fetch/parse failures, `no video variant`, `too many failed auto-restarts` | session produced no playable segments in time | retry in a minute, or download |

Every class names an action — a generic apology was the thing being replaced.
Adding a class: a `case` in `ClassifyError`, a row in
`TestClassifyError_StreamingChain`, the key in all 11 locale files, a row here.

## Reviewing the texts

Dev-only, on any resource page: `#action=stream&debug=error:<key>` renders the
key in the job log. `error.generic` should be rare enough to read as a bug
report; when a new message shows up in the Loki count above, it belongs in
the table.
