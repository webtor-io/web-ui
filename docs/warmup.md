# Torrent warmup & seeder fast-path

Warmup is the pre-flight step that runs between the user clicking *Stream* /
*Download* and the actual playback / file delivery. Two motivations:

1. **Pull head + tail bytes** so the transcoder / HTTP server can answer the
   first segment / range request without stalling.
2. **Measure download throughput** so we can compare against the required
   video bitrate (BT-slow modal) before the player starts.

Implemented in `jobs/scripts/action.go` (`warmUp`, `seederHasContent`,
`piecesCoverRange`). Bandwidth-check is `checkCachedRateLimit` /
`buildSlowDownloadData` in the same file.

## Three paths

| Path | Trigger | Behaviour |
|---|---|---|
| **Long-term cache** | `se.Meta.Cache=true` (rest-api S3 promotion) | Skip `warmUp` entirely. Bandwidth check runs cap-modal branch (plan-cap vs bitrate). |
| **Seeder fast-path** | `se.Meta.Cache=false` AND `seederHasContent(...)` returns true within `WARMUP_SEEDER_PROBE_TIMEOUT_SEC` (default 3s) | Silent skip — no `j.Skip` line, the job log goes straight to probe/render. Bandwidth check joins the cap-modal branch (same rationale: bottleneck has moved to plan-cap, not peers). |
| **Full warmup** | Everything else | Download `bandwidthTestSize` head + 500KB tail (stream) or 1MB head (download). Measure throughput. Bandwidth check runs BT-slow branch against the measured speed. |

## `seederHasContent` — fast-path probe

Common share-flow case: the sharer's torrent-web-seeder pod just served them,
the pieces are still resident, but the rest-api `Cache` flag is `false`
(promotion to S3 hasn't happened yet). The viewer arrives moments later and
otherwise pays the full warmup cost for nothing.

The probe opens the existing **stats SSE** (`torrent_client_stat` URL) and
waits for the **first** `statupdate` event. That event always carries the full
pieces array (see `torrent-web-seeder/server/services/stat.go:218-226` — the
diff machinery only kicks in from the second event onward). For each piece
covering `[0..head) ∪ [size-tail..size)` we check `Complete=true`. All present
→ skip warmup. Anything missing or empty / timed-out → fall through to the
full `warmUp`.

Piece size is approximated as `ceil(fileSize / len(Pieces))` because the
seeder proto only carries `position/complete/priority` (no per-piece byte
count). Rounding up means we sometimes require one extra piece at the edge —
fine, because we only short-circuit when *every* covering piece is complete,
so the worst case is a false negative that just falls through to full warmup.

## Why the fast-path goes through the cap-modal branch

Skipping `warmUp` means we have **no measured downloadSpeed**, so the BT-slow
gate (`downloadSpeed*8 < bitrate`) can't fire. That's fine — once the pod has
the bytes, the user's effective throughput is no longer torrent-peer-limited,
it's whatever plan-cap THP enforces (or the user's own connection). That's
*exactly* the existing `Cache=true` model: route to `checkCachedRateLimit`
which compares plan-cap vs bitrate. The `effectiveCache` local in
`streamContent` makes this collapse cleanly:

```go
effectiveCache := se.Meta.Cache
if !effectiveCache && !s.forceSlow && s.seederHasContent(...) {
    effectiveCache = true
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
