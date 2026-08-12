# JSON API

A documented HTTP API for paid accounts: the public Webtor resource API, plus
the account-scoped things rest-api knows nothing about — the library, the Vault
and profile preferences.

## Two halves, and why

```
handlers/api   routing, authorization, the endpoints
services/libapi  mount path, key middleware, host middleware, errors, account DTOs
services/api     the existing rest-api client — /resource, /list, /export go through it
models/          the library rows — same functions the library UI calls
services/vault   the same service the vault UI calls
```

| Half | Endpoints | Contract |
|------|-----------|----------|
| **Pass-through** | `/resource`, `/resource/{id}`, `/resource/{id}/list`, `/resource/{id}/export/{content_id}` | rest-api's, verbatim — paths, parameters and response bodies |
| **Account-scoped** | `/library`, `/vault`, `/profile` | ours; rest-api has no notion of an account |

The pass-through half returns **rest-api's own structs** (`ra.ResourceResponse`,
`ra.ListResponse`, `ra.ExportResponse`), not a re-modelled copy. That is the
whole point: code written against `api.webtor.io` works here with the base URL
and the auth header swapped, and a field added upstream appears here without a
change on this side. Anything that reshapes those payloads is a bug, not a
feature.

What the pass-through adds is **who is asking**: the request is re-signed with
claims built from the account behind the API key, so tier limits, rates and
grace apply exactly as they do in the web UI.

The library half is the same library WebDAV ([webdav.md](webdav.md)) and S3
([s3.md](s3.md)) serve — the same rows, so a torrent added here shows up in a
mounted drive immediately. Those two protocols expose it as a filesystem;
this API exposes it as what it is, a list of torrents.

> An earlier draft of this API mirrored the WebDAV/S3 filesystem instead
> (`/fs`, `/content`). It was dropped: it invented a third way to address the
> same content, and a client that wanted a stream URL had to walk a fake
> directory tree to get one that `/export` returns directly.

## Surface

Base URL: `https://webtor.io/api/v1` (or `https://api.webtor.io/v1`, see
[Dedicated hostname](#dedicated-hostname)).

| Method | Path | Scope | Notes |
|--------|------|-------|-------|
| `POST` | `/resource` | `api:write` | Store a `.torrent` or magnet, returns the id |
| `GET` | `/resource/{id}` | `api:read` | Name, size, file count, magnet |
| `GET` | `/resource/{id}.torrent` | `api:read` | The torrent file itself |
| `GET` | `/resource/{id}/list` | `api:read` | Files and directories (`path`, `output`, `limit`, `offset`, `sort`) |
| `GET` | `/resource/{id}/export/{content_id}` | `api:read` | Download / stream URLs (`types`, `output`, `archive-format`, `paths`, `imdb-id`) |
| `GET` | `/library` | `api:read` | Your torrents (`type=all\|movies\|series`, `sort=recent\|name\|year` — `year` needs a movies/series section, `limit`, `offset`) |
| `POST` | `/library` | `api:write` | Add a stored resource to the library |
| `GET` | `/library/{id}` | `api:read` | One entry; `404` = not in your library |
| `PATCH` | `/library/{id}` | `api:write` | Rename the entry |
| `DELETE` | `/library/{id}` | `api:write` | Remove from the library |
| `GET` | `/vault` | `api:read` | Points, content counters, pledges |
| `POST` | `/vault/pledges` | `api:write` | Pledge to a resource |
| `GET` | `/vault/pledges/{id}` | `api:read` | One pledge, with transfer status and progress |
| `DELETE` | `/vault/pledges/{id}` | `api:write` | Claim points back |
| `GET` | `/profile` | `api:read` | User, tier, scopes, preferences |
| `PATCH` | `/profile` | `api:write` | Partial preferences update |
| `POST` | `/device/code` | — | Start device authorization: `user_code` for the person, `device_code` for the machine |
| `POST` | `/device/token` | — | Poll for the key; `authorization_pending` / `slow_down` / `expired_token` until confirmed |
| `GET` | `/docs/index.html`, `/swagger.json` | — | Reference and spec (public) |

The intended flow is `POST /resource` → `POST /library` → `/list` → `/export`.
**Storing and adding to the library are separate on purpose**: the store is
global and content-addressed, the library is yours. Streaming something once
does not put it in your library, and removing it from your library does not
take it out of the store.

`POST /resource/` (trailing slash) is registered as well, because that is how
rest-api documents it and `RedirectTrailingSlash` is off globally.
`GET /resource/{id}.torrent` is routed inside the `{id}` handler, the same trick
rest-api uses.

## Device authorization

How a browserless client (CLI, TV app) obtains a key — RFC 8628 shaped.
`POST /device/code` (public, per-IP limited) returns a secret `device_code`
for the machine and a short `user_code` for the person, who confirms it at
`/device` on the main site (session + paid plan, per-user attempt limit
against code guessing). The machine polls `POST /device/token` at the
advertised `interval`; polling faster answers `slow_down`, and the key is
delivered **exactly once** — the parked row is deleted in the transaction
that returns it.

Every confirmation issues its own `access_token` row named
`device:<label> · <user_code>`, so the profile's "Connected devices" section
can revoke one device without touching the account's `api` key or the other
devices (`POST /device/revoke`, prefix-guarded so it can never delete a
non-device token). Pending authorizations live in `device_auth`
(migration 63), minutes-lived, purged opportunistically on code creation and
listed in the GDPR export like every user-keyed table.

## Authentication

`Authorization: Bearer <key>`, or `X-Api-Key: <key>` where a proxy strips
`Authorization`. The key is an `access_token` row named `api` with scopes
`api:read` / `api:write` — the same row shape as the WebDAV and S3 credentials,
so issuing and rotating reuse `at.Generate` / `at.Regenerate` untouched. It is
stored, not derived (unlike the S3 secret): a leaked key can only be rotated,
never recomputed.

Two things line up, and they are the non-obvious part:

1. **`libapi.RegisterAPIKeyMiddleware`** runs *before* `services/access_token`'s
   own middleware (both are global `r.Use`, so registration order in `serve.go`
   is the only lever). It puts the key from the header into `?token=<key>`.
   Everything downstream — `auth.UserContext`, claims, the rest-api client's
   claims — then works exactly as it does for WebDAV, with no API-specific
   plumbing. Only values that parse as a UUID are forwarded, because the
   access-token middleware treats a malformed token as a 500 and a typo in a
   curl command must not read as "webtor is broken".

   > **The token param is rewritten, never merged.** A caller who could smuggle a
   > `?token=` through the query string would be authenticated as its owner while
   > presenting their own key — i.e. read any account whose key id they know. The
   > middleware therefore deletes any caller-supplied value first, even when the
   > request carries no key at all, and `handlers/api.authorize` re-asserts that
   > the two agree. Regression test: `TestSuppliedTokenCannotOverrideHeader`.

2. **`handlers/api.authorize`** replaces `at.HasScope` + `claims.IsPaid` on this
   group. Same checks, but it answers with an error document: a client shown a
   bare 403 with an empty body cannot tell "no key" from "wrong plan", and those
   are fixed differently. Write methods additionally require `api:write`.

Keys are issued with both scopes today. The write path still checks its own —
otherwise a future read-only key would silently be able to delete library
entries (`TestAuthorizeRejectsWriteWithoutScope`).

## Errors

Every failure answers `{"error": {"code": …, "message": …}}`. **Clients branch on
`code`, not on the status**: a status says "you may not", a code says which of
the several reasons applies.

| Code | Status | Meaning |
|------|--------|---------|
| `unauthorized` | 401 | No key, or a key nobody owns |
| `forbidden` | 403 | Wrong scope, or the plan does not allow it |
| `payment_required` | 402 | Free plan |
| `not_found` | 404 | No such resource, file or library entry |
| `conflict` | 409 | Pledge already exists, or is still frozen |
| `bad_request` | 400 | Malformed body or query |
| `rate_limited` | 429 | Too many requests with this key; `Retry-After` says how long to wait |
| `upstream_error` | 502 | The services behind this one failed |
| `upstream_timeout` | 408 / 504 | A magnet could not be resolved in time, or an upstream call ran out of time |
| `unavailable` | 503 | Vault or the DB is not available on this deployment |
| `internal_error` | 500 | Ours |

`upstream_*` is deliberately separate from `internal_error`: those are usually
worth retrying and `internal_error` is not.

The rest-api client flattens upstream statuses into plain errors, so a message
substring is the only signal left about what actually happened. `upstreamError`
matches **the same substrings rest-api's own error middleware matches** to pick
its status (`not found`, `forbidden`, `failed to parse`, `timeout`,
`deadline exceeded`), which reproduces upstream's decision instead of inventing
a second, divergent one. The upstream text itself stays in the logs and never
reaches the response body — it carries internal URLs. Covered by
`TestUpstreamErrorMapsToUpstreamStatus`.

Two consequences of the same flattening are worth knowing:

- **A missing resource reaches `/list` as an empty listing, not as an error** —
  the client turns upstream's 404 into a zero value. Answering `200` with an
  empty `items` for a typo'd infohash is indistinguishable from an empty
  directory, so an empty listing is confirmed against `GET /resource/{id}`
  (cached) before it is served, and a resource that is not there answers 404.
- **Infohashes are lower-cased on every path that takes one**
  (`normalizeResourceID`), matching what rest-api does. Without it the same
  torrent could exist under two identities — one in the store and the library,
  another in the Vault — and a delete issued in the other case would miss.

## Library

`type` maps onto the same queries the library UI's tabs use
(`models.GetLibrary{,Movie,Series}TorrentList`), so `movies` means exactly what
the Movies tab shows: a torrent with at least one recognized film. Classification
comes from enrichment, which runs in the background after `POST /library` — a
torrent added a second ago is real, listed under `all`, and not yet a "movie".

Paging is done in memory. Those queries have no `LIMIT` (the UI reads the whole
list to render counts), and a library large enough for that to matter does not
exist yet — worth revisiting the day it does, in `models/` rather than here.

`POST /library` fetches and parses the stored torrent rather than trusting the
request: the row carries name, size and file count, and those have to come from
the metainfo the store actually holds. It is idempotent — a resource already in
the library answers **200** with the existing entry, and only a real insert
answers **201**, so a retry after a timeout is both safe and honest about what
it did.

`DELETE` is **not** idempotent: removing something that is not there answers
404. Nothing here does copy-then-delete (the reason S3 has to answer 204), and a
script deleting an id that is not in the library has a bug worth surfacing.

`PATCH /library/{id}` renames the entry everywhere it is shown — library UI,
WebDAV, S3 — because they all read that row.

Membership is resolved with `models.GetLibraryByResourceID`, which returns nil
for both "no such row" and "someone else's row". The API must not distinguish
them: the difference is not the caller's business, and answering differently
would confirm that another account holds a given infohash.

## Vault pledge status

Between "pledged" and "vaulted" the transfer is observable only through
`GET /vault/pledges/{resource_id}`: `status` walks
`waiting → queued → storing → vaulted`, with `progress` (percent) and
`stored_size` / `total_size` (bytes) while a transfer is measurable. The
vocabulary is deliberately finer than the UI's single "vaulting" badge — an
API client can act on the difference between a stuck `queued` and a `failed`,
a viewer cannot.

Two statuses need their semantics spelled out. **`failed` is terminal for the
attempt, not for the resource**: storage retries on its own schedule, so the
right reaction is to keep polling, not to re-pledge (there is nothing to
re-pledge — the pledge is fine). `expired` means the resource lost its
funding. Progress advances on the order of tens of seconds on the storage
side, so polling faster than every 10 seconds only reads the same numbers
again.

The status resolution is `libapi.NewPledgeStatus`, a pure function pinned by
`services/libapi/vault_test.go` — including the lag case where storage reports
finished before the completion event lands in our DB (that answers `vaulted`:
report the truth, not the lag).

## Export selection: `types` and `output`

`types` is rest-api's parameter, honored here with its exact semantics: a CSV
of export names, absent meaning all, an unknown name a `400`, and an export
that does not apply to the file silently absent from the response. It is what
makes an export call portable between the two hosts.

`output` predates the alignment and stays because it answers a question
`types` cannot: "give me exactly this one, and error if it is not there" —
a missing export answers `404` instead of an empty map. The two are mutually
exclusive (`400` when both are given): merging them would need a combined
semantics neither contract defines.

## Rate limiting

Requests are limited per key — sustained `API_RATE_LIMIT` per second with an
`API_RATE_BURST` burst (defaults 10 and 50; `0` disables). Past the burst the
answer is `429` with code `rate_limited` and a `Retry-After` header, counted
in seconds.

This limits *requests to the API*; it is separate from the tier limits on the
streaming chain, which meter traffic, not calls. The bucket lives in each
replica's memory, so the effective ceiling is the configured number times the
replica count — accepted: the limit exists to turn a runaway loop or a leaked
key into `429`s, not to meter usage. Limiting happens by key string before the
key is proven valid, so hammering with a wrong key is bounded the same way.

## Export URLs are short-lived

`/export` hands back URLs the streaming chain serves, each carrying its own
authorization and an expiry. **Resolve them when you are about to use them; do
not store them.** `output=…` filters the response to one export — the filtering
happens here rather than upstream, because the client we proxy through always
asks for the full set and everything is already in hand by the time we can
filter.

## Dedicated hostname

`API_DOMAIN=<host>` serves the API at that host's root; production uses
`api.webtor.io` (taken over from torrent-http-proxy on 2026-08-08 — its
clients moved to their own hostname). The DNS record is deliberately
NOT Cloudflare-proxied: CF bot challenges answer "Just a moment..." HTML
to programmatic clients.
`libapi.RegisterHostMiddleware` rewrites those requests onto `/api` and
re-dispatches, so:

```
https://api.webtor.io/v1/library   ==   https://webtor.io/api/v1/library
```

**The version stays in the path.** Dropping it (`api.webtor.io/library`) would
leave the dedicated host with no way to address a future `/api/v2`, and clients
pinned to "the host" rather than to a version is exactly the breakage a version
prefix exists to prevent.

Doing the rewrite in the app rather than in nginx keeps one moving part: the
session middleware's CSRF exemption and the API-key middleware are both keyed on
the `/api` prefix it produces, and a proxy-side rewrite would have to be kept in
sync with two things it cannot see. Registering a host also requires exempting
it from the canonical-domain redirect (`w.RedirectNonCanonical` in `serve.go`),
or every API request answers 302.

Unlike S3, nothing here signs the request path, so a header-rewriting CDN in
front of the endpoint is harmless — the dedicated host is about ergonomics, not
about keeping a proxy out of the path.

## Swagger

Annotations live on the handlers in `handlers/api`; the generated spec is
committed under `docs/swagger` and served at `/api/v1/swagger.json`, with the UI
at `/api/v1/docs/index.html`. Both are public — an API reference you need a key
to read is a reference nobody evaluates before signing up.

Regenerate after touching an annotation:

```
go install github.com/swaggo/swag/cmd/swag@latest
make swagger
```

`--parseDependency` is what pulls rest-api's response types into the spec, so
the documented `services.*` schemas are literally upstream's.

> **`docs/swagger` is compiled into the binary, unlike the rest of `docs/`.**
> `.dockerignore` drops `docs/` wholesale — it was prose until this landed —
> which made the build succeed locally and fail only inside the image with
> *"no required module provides package …/docs/swagger"*. The directory is
> excepted there (`!docs/swagger/`); keep the exception if either path moves.

> **The instance name must not be swaggo's default.** Every generated docs
> package registers itself globally by instance name at `init`, and rest-api's
> own spec — linked into this binary through `services/api` — already claims
> `swagger`. Two packages under one name **panic the process on startup**, not at
> the first request. Hence `--instanceName libraryapi`, the matching
> `ginSwagger.InstanceName` in `handlers/api/docs.go`, and the generated
> `SwaggerInfolibraryapi` symbol.

> **The UI route defends itself against path-rewriting middleware.** ginSwagger
> matches asset names against `RequestURI`, but serves them through a
> package-global webdav handler that strips `URL.Path` — with the strip-prefix
> frozen from the *first* matched request. Any middleware that rewrites
> `URL.Path` and leaves `RequestURI` intact (the i18n language prefix, the
> api-host rewrite) made the two disagree: every UI asset answered 404 and the
> reference rendered as a blank page until restart. `registerDocs` now aligns
> `RequestURI` with `URL.Path` before delegating, and `/api/` is excluded from
> language routing altogether (`services/i18n/middleware.go`) — API URLs are
> never language-prefixed, same as `/assets/` or `/token/`. Regression tests:
> `handlers/api/docs_test.go`.

### Key prefill

A logged-in reader gets "Try it out" preauthorized with their own key: a
snippet appended to `swagger-initializer.js` (`prefillJS` in
`handlers/api/docs.go`) fetches `GET /api-credentials/key` — session-only,
`Cache-Control: no-store`, `{"key": "..."}` or `204` when no key is issued —
and calls `ui.preauthorizeApiKey("BearerAuth", "Bearer <key>")`. The endpoint
sits outside the `IsPaid` gate on purpose: the profile page shows an issued key
to a lapsed account too, and the API itself still answers 402 to it. Everything
fails closed to the pre-existing behaviour (empty Authorize dialog): anonymous
readers get 401, the dedicated api host has neither the session cookie nor the
`/api-credentials` route, and any fetch error is swallowed. No new exposure
surface — the same session can already read the key off the profile page.

`registerDocs` overwrites the spec's host and base path at startup from
`libapi.PublicEndpoint`, so "Try it out" hits the origin this deployment
actually serves — a dedicated host or `<domain>/api/v1`.

## Configuration

| Flag / Env | Default | Description |
|------------|---------|-------------|
| `--disable-api` / `DISABLE_API` | off | Skips route registration entirely; the profile hides the block |
| `--api-domain` / `API_DOMAIN` | empty | Comma-separated hostnames serving the API at their root |
| `--api-rate-limit` / `API_RATE_LIMIT` | 10 | Sustained requests per second allowed per key, per replica (0 disables) |
| `--api-rate-burst` / `API_RATE_BURST` | 50 | Request burst allowed per key on top of the sustained rate |

There is no new table and no new secret: the key is an `access_token` row, which
the GDPR export already covers (`docs/data_export.md`).

## Testing

- `services/libapi/middleware_test.go` — the key-override invariant, header
  parsing, and the dedicated-host rewrite.
- `handlers/api/handler_test.go` — the authorize matrix (anonymous, unknown key,
  free plan, missing write scope, foreign scope) and that the docs actually
  serve the spec under the named instance.

Manual smoke test against a local instance:

```
KEY=<from the profile page>
curl -H "Authorization: Bearer $KEY" -d 'magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10' \
     localhost:8080/api/v1/resource
curl -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
     -d '{"resource_id":"08ada5a7a6183aae1e09d831df6748d566095a10"}' localhost:8080/api/v1/library
curl -H "Authorization: Bearer $KEY" 'localhost:8080/api/v1/library?type=movies'
curl -H "Authorization: Bearer $KEY" 'localhost:8080/api/v1/resource/08ada…a10/list?output=tree'
curl -H "Authorization: Bearer $KEY" 'localhost:8080/api/v1/resource/08ada…a10/export/0?output=download'
```
