# Torrent warmup & seeder fast-path

Warmup is the pre-flight step that runs between the user clicking *Stream* /
*Download* and the actual playback / file delivery. Two motivations:

1. **Prioritise head + tail pieces** on the seeder pod so the transcoder /
   HTTP server can answer the first segment / range request without stalling.
2. **Measure download throughput** so we can compare against the required
   video bitrate (BT-slow modal) before the player starts.

Implemented in `jobs/scripts/action.go` (`warmUp`, `piecesCoverRange`).
Bandwidth-check is `checkCachedRateLimit` / `buildSlowDownloadData` in the
same file.

## Three paths

| Path | Trigger | Behaviour |
|---|---|---|
| **Long-term cache** | `se.Meta.Cache=true` (rest-api S3 promotion) | Skip `warmUp` entirely. Bandwidth check runs cap-modal branch (plan-cap vs bitrate). |
| **Seeder fast-path** | `se.Meta.Cache=false` AND (stats first event shows head+tail pieces complete OR seeder vault/cache short-circuits `?warmup` with an empty SSE) | Silent skip — no `j.Skip` line, the job log goes straight to probe/render. Bandwidth check joins the cap-modal branch (same rationale: bottleneck has moved to plan-cap, not peers). |
| **Full warmup** | Everything else | Open `?warmup` SSE for the head range (`bandwidthTestSize` for stream, 1MB for download) and tail range (500KB on stream only) in parallel. Seeder bumps `PiecePriorityHigh` on covered pieces and streams a cumulative downloaded counter; web-ui derives throughput from that. Bandwidth check runs BT-slow branch against the measured speed. |

## Fast-path probe — inlined in `warmUp`

Common share-flow case: the sharer's torrent-web-seeder pod just served them,
the pieces are still resident, but the rest-api `Cache` flag is `false`
(promotion to S3 hasn't happened yet). The viewer arrives moments later and
otherwise pays the full warmup cost for nothing.

The probe lives **inside `warmUp` itself**, not as a separate function. It
reuses the **stats SSE** subscription that `warmUp` was already opening for
the UI peer-counter:

1. `warmUp` opens `api.Stats(warmupCtx, statsURL)` once.
2. It reads the **first** `statupdate` event synchronously, with a
   `SeederProbeTimeoutSec` deadline (default 3s).
3. That first event always carries the full pieces array (see
   `torrent-web-seeder/server/services/stat.go:218-226` — the diff machinery
   only kicks in from the second event onward). For each piece covering
   `[0..head) ∪ [size-tail..size)` we check `Complete=true`. All present
   → return early with `cached=true`; no `j.InProgress` line is ever
   emitted. Anything missing → fall through to the full warmup; the consumed
   first event is also surfaced to the UI so the peer-counter starts
   immediately instead of waiting for event #2.
4. If the probe deadline expires before any event arrives, the full warmup
   runs normally and the same Stats channel is handed off to the UI goroutine.

This single-subscription design is **load-bearing**: an earlier version that
opened a separate `Stats()` call from a standalone `seederHasContent` helper
doubled the per-session SSE subscriber rate. Each `Stats()` call allocates a
1MB `bufio.Scanner` buffer (needed for high-piece-count torrent manifests),
and `Stats.func1` rose to **~24 % of the heap** under prod traffic, pushing
the 2Gi-limited pods into OOMKilled cycles within hours of deploy. Folding
the probe into `warmUp` cuts the per-session Stats opens back to one. Do not
revert to a separate probe-side `Stats()` call without solving the memory
amplification first.

Piece size is approximated as `ceil(fileSize / len(Pieces))` because the
seeder proto only carries `position/complete/priority` (no per-piece byte
count). Rounding up means we sometimes require one extra piece at the edge —
fine, because we only short-circuit when *every* covering piece is complete,
so the worst case is a false negative that just falls through to full warmup.

## `?warmup` SSE — bandwidth-free byte priming

Before sha-3fa59da (torrent-web-seeder) the "full warmup" path issued two
`Range` GETs (head + tail) over the proxy chain and discarded the body —
purely to make anacrolix pull those bytes from peers. That wasted bandwidth
end-to-end (peers → seeder → THP → web-ui, all to feed `io.Discard`) and
told anacrolix about the head range only after it was already most of the
way downloaded.

The seeder now exposes a `?warmup` SSE endpoint on the same URL path as
`?stats`. It parses the `Range` header, calls `Piece.SetPriority(High)` on
every piece overlapping the range, and emits `data: <downloaded>\n\n` once
per second where `downloaded` is the count of bytes-within-range that
anacrolix has already verified. Stream close = warmup complete. The bytes
themselves are never transferred. `web-ui`:

- Derives the warmup URL by swapping `stats=true` → `warmup=true` on the
  rest-api-signed stats URL (same path, same auth tokens, same pinned
  non-premium domain — see `api.Warmup` in `services/api/api.go`).
- Opens head and tail SSE streams in parallel so both priority bumps land
  up front and anacrolix can dispatch peer requests for both ranges
  concurrently. The tail stream is best-effort; a failure there just logs
  and continues. Head failure surfaces as `failed to warmup`.
- Computes `downloadSpeed` from the combined head+tail counter: latches a
  `(timestamp, bytes)` measurement window once total downloaded crosses
  `skipBytes` (slow-start skip, same threshold the old `speedReader` used),
  then divides `(final - measureStartBytes) / elapsed` at SSE close.

**Throughput semantics shifted slightly.** The old path measured the
end-to-end byte rate (peers → seeder → THP → web-ui). The new SSE counter
measures peer → seeder throughput only. For the BT-slow gate this is
strictly more correct — that gate exists to catch "the swarm can't feed
this bitrate", and seeder-side throughput is the closest signal. In
practice the two are equivalent today because the warmup `?download=true`
URL was unmoderated (no THP rate-limit on the warmup path).

**Vault/cache short-circuit.** The seeder responds with `200 OK` and an
empty SSE body when `availableWithoutTorrent(...)` is true (file fully
served from local file-cache or vault), then closes. `web-ui` detects this
as "head SSE closed with zero `data:` events" and treats the result as
`cached=true`, routing the bandwidth check through the cap-modal branch —
same outcome as a stats-probe hit, just for the broader vault/cache case
the piece-array probe can't see.

**Why not drop the stats-probe now that `?warmup` reports completion?**
The probe completes in one round-trip on a single, already-open SSE; the
`?warmup` path needs two new TCP sessions plus their HTTP/2 stream
windups before it can report the same "all pieces present" verdict. Keep
both: probe first (cheapest hit), `?warmup` for the broader case.

## Why the fast-path goes through the cap-modal branch

When `warmUp` returns `cached=true`, we have **no measured downloadSpeed**, so
the BT-slow gate (`downloadSpeed*8 < bitrate`) can't fire. That's fine — once
the pod has the bytes, the user's effective throughput is no longer
torrent-peer-limited, it's whatever plan-cap THP enforces (or the user's own
connection). That's *exactly* the existing `Cache=true` model: route to
`checkCachedRateLimit` which compares plan-cap vs bitrate. The `effectiveCache`
local in `streamContent` makes this collapse cleanly:

```go
effectiveCache := se.Meta.Cache
if !effectiveCache {
    if s.forceSlow {
        j.Skip(...)
    } else {
        var hit bool
        downloadSpeed, hit, err = s.warmUp(...)
        if err != nil { return }
        if hit { effectiveCache = true }
    }
}
// ...later:
} else if effectiveCache && !graceMode {
    // cap-modal
} else if !effectiveCache && downloadSpeed > 0 {
    // BT-slow
}
```

## Tuning

| Env / flag | Default | Notes |
|---|---|---|
| `WARMUP_TIMEOUT_MIN` / `--warmup-timeout-min` | 3 | Hard ceiling on full warmup. |
| `WARMUP_NO_PEERS_TIMEOUT_SEC` / `--warmup-no-peers-timeout-sec` | 60 | Surface `no_peers` early when zero bytes & zero peers. Gated on stats-ever-seen so a cold-start SSE doesn't mis-fire. |
| `WARMUP_SLOW_PEERS_TIMEOUT_SEC` / `--warmup-slow-peers-timeout-sec` | 120 | Surface `no_peers` when bytes < 1MB after this window. |
| `WARMUP_SEEDER_PROBE_TIMEOUT_SEC` / `--warmup-seeder-probe-timeout-sec` | 3 | Fast-path probe budget. 0 disables the probe entirely (kill-switch). False negatives are safe — caller falls through to full warmup. |

## Edge cases

- **Probe timeout = 0** — disables the fast-path. Restores pre-2026-05-25 behaviour.
- **404 on stats URL** — `api.Stats` wraps as `errors.New("cached")`. The
  probe treats it as a cache hit; in practice this shouldn't fire because
  `se.Meta.Cache=true` short-circuits the branch before we get there.
- **Pieces complete but pod evicts between probe and stream-start** — race
  is possible but unlikely (no aggressive eviction during active sessions);
  the player would just see slow first byte and recover.
- **`forceSlow=true`** — skip the probe; the user already accepted slow
  playback, no need to optimise.
- **Very small files** — head+tail can overlap; `piecesCoverRange` clamps the
  tail start at `headPieces` so overlapping ranges still validate correctly.
- **`?warmup` empty SSE response** — seeder vault/local-cache hit; web-ui
  treats this identically to a probe hit (`cached=true`, cap-modal branch).
- **`?warmup` tail failure** — best-effort; logged and ignored. The head
  range still drives the throughput measurement and the watchdog.
- **`?warmup` head failure (404 / 500 / network)** — logged and downgraded
  to "no warmup performed". The transcoder/HTTP path pulls the head cold;
  the watchdog still surfaces `no_peers` if the swarm is actually dead.
  Unlike `Stats`, the seeder's `?warmup` handler never uses 404 as a
  cache signal — 404 means `findFile` couldn't resolve the path on this
  pod, not "content is cached". `api.Warmup` therefore treats all non-200
  responses as a single transient error.
