# User-facing errors

How an `error` becomes the sentence a person reads, and which sentences exist.

## Mechanics

`services/web/user_error.go`:

- `UserError{Key, Err}` — an error that already knows its key (`NewUserError`).
- `ClassifyError(err)` — everything else: a substring switch over `err.Error()`,
  falling back to `error.generic`. Order matters: the more specific wrappers
  (`forbidden`, `not_found`, `resolution_not_supported`, `status=415`) sit above
  the broader streaming-chain classes.
- `StatusForErrKey(key)` — HTTP status for the error page; retry-able classes
  (`service_unavailable`, `upstream_unavailable`) answer 503.

Render points: the job progress log (`jobs/jobs.go` errorFormatter), the error
page (`services/web/middleware.go`), redirects with `?err=` (`services/web/helper.go`).
Every render logs one structured line — `user error shown` with `err_key` and
`surface=job|page` — so the distribution, and the share still landing in
`error.generic`, can be read off Loki:

```
{app="web-ui"} |= "user error shown" | logfmt | err_key != ""
```

## Streaming-chain classes (2026-09)

| Key | Matches | What happened | What the user can do |
|---|---|---|---|
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
