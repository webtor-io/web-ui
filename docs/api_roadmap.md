# JSON API roadmap

Where [the JSON API](api.md) goes next, in order. The list came out of an
August 2026 review of the surface against what integrators (CLI tools, mobile
apps, media-center add-ons) actually need, and against the public APIs of
comparable services (put.io, Real-Debrid, Premiumize, AllDebrid, TorBox).

The review's conclusion worth keeping: **ecosystems grow from onboarding and
contracts, not from features**. Services with comparable feature sets differ
wildly in third-party adoption, and the ones with adoption all have a public
spec, a machine-readable error contract, and a way for a device to obtain a key
without a browser copy-paste. The spec and the error contract we have; the
rest is this list.

## Shipped (2026-08-08)

The original "Now" block went out in one release, together with the Swagger
reference on its own host (`api.webtor.io`), key prefill for logged-in
readers, CORS for browser callers and 429 documentation on every endpoint:

1. **Pledge transfer status** — `GET /vault/pledges/{resource_id}`.
2. **`types` export selection** with upstream's exact semantics (and
   rest-api's spec corrected to document it).
3. **Per-key rate limit** — 429 + `Retry-After`, `rate_limited` error code.

## Shipped (2026-08-09)

**Device/PIN key issuance** — `POST /device/code` + `/device/token` (RFC 8628
shaped), confirmation page at `/device`, per-device keys with revocation from
the profile. See [api.md](api.md#device-authorization).

## Now (next up)

### Completion callbacks

An optional `callback_url` on a pledge: one POST when the transfer finishes or
fails, following the Standard Webhooks spec. The internal event already
exists; this forwards it.

## Original "Now" notes (kept for context)

### 1. Pledge transfer status — `GET /vault/pledges/{resource_id}`

The biggest observability gap: between "pledged" and "vaulted" an API client is
blind. The web UI shows live progress over a session-bound SSE stream that a
machine client cannot use (CSRF token in the query, session cookie). The
storage backend already tracks state and byte-level progress; this endpoint
exposes it — `status`, `progress`, `stored_size` / `total_size` — per pledge,
under the same key auth as everything else.

### 2. One export-selection parameter that works everywhere

`/export` here reads `output`; rest-api's code reads `types` (a CSV list) while
its published spec *documents* `output` — so the "swap the base URL and it
works" promise breaks in both directions. Fix: this API accepts `types` with
upstream's exact semantics, rest-api's spec is corrected to document `types`,
its undocumented auth parameters, and `/speedtest`.

### 3. Per-key request rate limit

Traffic through the streaming chain is already limited by tier claims; requests
to the API itself are not limited at all. A leaked key or a runaway loop in an
integrator's script should degrade into `429` + `Retry-After`, not into load on
everything behind the API. Applies per key, answers with a `rate_limited` error
code.

## Later, roughly in order

- **Async magnet resolve.** `POST /resource` blocks up to 3 minutes while a
  magnet resolves against the network — longer than mobile OS and proxy
  timeouts. Add `202` + a status resource for the resolve job; keep the
  synchronous path for compatibility.
- **Content readiness as data.** Today "fully cached" is expressed by the
  *absence* of a `torrent_client_stat` export. Expose it as a field, and
  expose the transfer stats (progress, peers) as a documented JSON endpoint
  instead of an undocumented SSE stream.
- **Delta polling.** A `session` + `counter` pair on status endpoints so a
  client gets only what changed since its last poll — live progress bars
  without WebSocket infrastructure and without hammering the full status path.
- **Stable file permalinks.** A URL carrying key + resource + file that
  redirects to a fresh export URL on each hit, so integrators can embed links
  that outlive the short-lived export URLs they currently must re-resolve.

## Explicitly rejected

- **Mandatory file selection before download** (Real-Debrid's
  `selectFiles`): an artifact of the "download everything into the cloud
  first" model. Streaming on demand already solves what that step exists for.
- **A server-side transcode farm.** On-the-fly HLS is the right architecture;
  services that built transcode farms have been retiring them.
- **A filesystem-shaped API surface.** Rejected once already (see
  [api.md](api.md)); WebDAV and S3 are the filesystem views.
