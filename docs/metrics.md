# Prometheus metrics

`web-ui` exposes Prometheus metrics on the platform's metrics port (`8083`,
`/metrics`) through `common-services` (`cs.RegisterPromFlags` / `cs.NewProm`),
the same way `torrent-http-proxy` does. Flags: `USE_PROM` (default on),
`PROM_HOST`, `PROM_PORT`. The Helm chart already declares the `httpprom`
container port and a `ServiceMonitor`, so a deploy of this code is scraped
without an infra change.

The collectors live in `services/metrics`, on the default registry, namespace
`webui`.

## Metrics

| Name | Type | Labels | Meaning |
|---|---|---|---|
| `webui_http_requests_total` | counter | `route`, `method`, `status` | Requests answered by the Gin engine |
| `webui_http_request_duration_seconds` | histogram | `route`, `method` | Wall time per request; buckets 5 ms … 30 s |
| `webui_http_requests_in_flight` | gauge | — | Requests currently in the handler chain |
| `webui_panics_total` | counter | `route` | Handler panics recovered by `web.RecoverToLog` |
| `webui_jobs_total` | counter | `job`, `outcome` | Async job script runs, `outcome` ∈ `ok` / `error` / `rejected` |
| `webui_jobs_in_flight` | gauge | — | Job scripts currently executing |
| `webui_stremio_paywall_video_total` | counter | `lang`, `method` | Stremio playback clicks answered with the paywall clip (`lang` of the clip; `HEAD` is Stremio's pre-play probe, not a view) — docs/stremio.md |
| `webui_trial_shortlink_total` | counter | `target`, `campaign`, `from` | Visits to `/trial`: `target` ∈ `checkout` / `donate` / `none` (nothing on sale), `campaign` ∈ `paywall` / `none` / `other` from `utm_campaign`, `from` ∈ the site surfaces of `offer.TrialFroms` / `none` / `other` from `?from` (docs/offers.md, "The trial link") |

### Label rules

Every label is drawn from a set fixed at build time. Nothing from a request
(paths with infohashes, query values, user ids) may become a label — each new
value would be a series kept for the life of the process.

- `route` is the router's template (`c.FullPath()`, e.g. `/:resource_id/status`),
  never the concrete path. A request no route claims (404s, probes for
  `/wp-login.php`) is `unmatched`. The language prefix is stripped by the
  i18n HTTP middleware before routing, so `/ru/profile` and `/profile` share
  a series.
- `method` is the request method when it is one the router can answer
  (standard verbs plus the WebDAV set); anything else is `other`.
- `job` is the queue name: `load`, `enrich`, `embded`, `payment`, and the
  action names (`stream-video`, `stream-audio`, `download`, `preview-image`).
- `lang` on the paywall counter is one of the locale codes a clip was rendered
  for (the English fallback included), never the account's raw setting;
  `campaign` on the trial counter collapses every `utm_campaign` but
  `paywall` and the empty one into `other` — the value is client-supplied.
  So is `from`: the surfaces the site's own trial links name
  (`offer.TrialFroms`: `promo-banner`, `download-nudge`, `limit-modal`,
  `grace`, `no-peers`, `onboarding`, `donate`) keep their name, a visit
  without the parameter is `none`, anything else `other`. Both are bounded
  inside `metrics.TrialShortlink`, which takes the raw query values — at
  most 3 × 3 × 9 = 81 series. A new surface is a new
  entry in `offer.TrialFroms`, never a value passed through.
- `rejected` is a torrent-store stoplist block — working as intended, so an
  error-rate alert on `outcome="error"` does not fire on a burst of blocked
  hashes.

### What is deliberately not measured

- **Held-open routes are excluded from the duration histogram.** A request
  whose response stays open for as long as the client listens measures the
  viewer's patience, not the server, and would pin every percentile at the
  top bucket. They are still counted in `requests_total` and in the in-flight
  gauge. The exclusion is a mark on the route definition
  (`metrics.Streaming` as the first handler), not a sniff of the
  `Content-Type`, so a handler that fails before setting headers cannot slip
  into the histogram. Marked today:
  - `GET /queue/:queue_id/job/:job_id/log` (job log SSE)
  - `GET /:resource_id/status` (status badge SSE)
  - `GET /discover/ai/chips/stream`, `/discover/ai/recommend/stream`,
    `/discover/ai/refine/stream` (AI recommendations SSE)

  Adding another SSE / long-poll route means adding `metrics.Streaming` to
  it. WebDAV and S3 are not on the list: torrent content is answered with a
  redirect to the streaming chain, only small `.torrent` bodies are served
  inline.
- **Job replays are not runs.** A job id that already has a stored result
  (another replica ran it) is replayed from storage and does not touch
  `webui_jobs_total`. A failure is counted once, at the script's return, even
  though the `got job error` log line is written twice (in `Job.Error` and
  again in `Jobs.retire`). A script that panics counts as `error`.
- **Requests the i18n HTTP middleware answers itself** (the language 302
  before Gin routing) are outside the engine and not counted.

## Placement of the middleware

`metrics.Middleware()` is installed *outside* `gin.CustomRecovery`:

```go
r.Use(gin.Logger(), metrics.Middleware(), gin.CustomRecovery(w.RecoverToLog))
```

The status is read once the handler chain returns, and a panicking chain
returns through recovery, which writes the 500. Placed inside recovery the
middleware would unwind first and count the request as a 200.
`TestMiddleware_PanicIsCountedAs500` fails if the order is swapped.

## Local development

`webpack-dev-server` listens on `8083` — the same port `PROM_PORT` defaults
to. With both running the Go process fails to bind and exits (`cs.Serve`
treats a listener error as fatal). Set `USE_PROM=false` or `PROM_PORT=8084`
in the run configuration.
