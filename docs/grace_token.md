# Grace Token

Free users get **20 minutes of movie-time at 50 Mbps** on each torrent regardless of pause/resume/reload. After the grace window, segments fall back to the user's plan-cap rate. No walltime, no Redis state on the hot path — the only state is the user's signed primary JWT (with a `rules` claim) and the HLS manifest's `#EXT-X-SESSION-OFFSET` tag.

## Why

Pre-Sprint-2 the bandwidth gate raised an insulting cap-modal for free users on cap-edge files. Sprint 1.5 metrics: 70% of anon-free chose "Continue at slow speed", 2.7% upgrade. Sprint 2 replaces the upfront block with: full speed for the first 20 min, then a soft CTA. Conversion is the metric.

Walltime grace was rejected (pause/resume fraud). Adding state to claims-provider was rejected (it stays read-only). Grace is therefore movie-time-bound and fully stateless on the segment path.

## Architecture

```
[player] ──GET m3u8──► [THP] ──proxy──► [content-transcoder]
                       ▼                        │
                       │   reads #EXT-X-SESSION-OFFSET
                       │   reads rules from primary JWT
                       │   rewrites segment URLs:
                       │     - movie_time < grace_dur → grace_token
                       │     - movie_time ≥ grace_dur → primary_token
                       ▼
[player] ──GET segment──► [THP] ──token validate + rate-limit──► [origin]
                                  rate=50M if grace_token + hash matches
                                  rate=tier_rate otherwise
```

Two signed JWTs are involved:

- **Primary token** — signed by web-ui (in `prepareRequest`) into the X-Token header on every rest-api call. rest-api copies the same string verbatim into the `?token=` query of every signed export URL it returns. Carries identity, plan rate, and the new `rules` claim.
- **Grace token** (new) — separate signed JWT issued by web-ui per (user, torrent_hash). Carries `kind=grace`, `hash`, `rate=50M`. No expiry. Travels inside the primary token's `rules` claim.

Both signed with the shared `WEBTOR_API_SECRET` (HS256). Note that web-ui owns URL-token signing end-to-end — rest-api is a pass-through for the X-Token header, never re-signing.

## Token & rule shape

Primary JWT claims (additions):
```json
{
  "rate": "5M",
  "role": "free",
  "rules": [
    {
      "kind": "grace",
      "scope": "manifest",
      "duration_sec": 1200,
      "token": "<GRACE_JWT>"
    }
  ]
}
```

Grace JWT claims:
```json
{
  "rate": "50M",
  "role": "grace",
  "hash": "<torrent_hash>",
  "kind": "grace"
}
```

## Per-service responsibilities

### content-transcoder

Emits `#EXT-X-SESSION-OFFSET:<seek_seconds>` tag at the top of every variant playlist. Tells THP the movie-time of segment 0 so THP can compute per-segment movie-time without session-state lookups. Players ignore unknown `#EXT-X-*` tags (RFC 8216 §3.1).

Files:
- `services/session.go` `PlaylistForStream` — variant injection
- `services/web.go` `sessionPlaylistHandler` — master playlist injection

### torrent-http-proxy

Two responsibilities:

1. **Manifest rewriting.** When proxying a `.m3u8` response and the request's primary JWT carries a grace rule, walk the playlist's `#EXTINF`/segment pairs, accumulate movie-time from `#EXT-X-SESSION-OFFSET`, and replace `?token=PRIMARY` with `?token=GRACE` on every segment whose movie-time start falls within `[0, duration_sec)`.

2. **Hash binding.** When validating a segment request whose token has `kind=grace`, reject if the bound `hash` doesn't match the request's torrent hash. Prevents replay across content.

Files:
- `services/claims.go` — `Rule` struct, `Rules []Rule` on `StandardClaims`, `ExtractRules` helper
- `services/manifest_rewriter.go` — `RewriteManifest`, `ManifestContext` plumbing, `parseSessionOffset`, `findGraceRule`, `swapToken`. Pure functions, easy to test.
- `services/http_proxy.go` `modifyResponse` — calls `maybeRewriteManifest` for `.m3u8` paths
- `services/web.go` `proxyHTTP` — sets `ManifestContext` on the request before `pr.ServeHTTP`; rejects mismatched grace tokens

### web-ui

Issues grace tokens for free-tier users (anon + authenticated `role=free|nobody`) and attaches them as a `Rules` field on the outgoing primary `Claims`. The X-Token header carrying these claims is copied verbatim by rest-api into every signed export URL — so rules ride to THP without any URL re-signing on our side. Removes the cap-modal cached-rate branch under grace mode (BT-slow check still fires for non-cached content because grace rate doesn't help if the user's own internet is slow).

Files:
- `services/api/api.go` — `secret` field on `Api`; `Rules []Rule` field on the outgoing `Claims` struct
- `services/api/grace.go` — `Rule`, `GraceClaims`, `SignClaims` (signs an arbitrary `jwt.Claims` payload; used to mint the inner grace token)
- `jobs/jobs.go` — three CLI flags (`GRACE_RULES_ENABLED`, `GRACE_DURATION_SEC`, `GRACE_RATE`) wired into `GraceSettings`
- `jobs/scripts/grace.go` — `GraceSettings`, `isFreeTier(c)`, `applyGraceRules(sc, hash, c)`: signs the inner grace token and sets `c.ApiClaims.Rules` BEFORE the export call, plus surfaces `GraceDurationSec`/`GraceFreeRateMbps` for the template/JS
- `jobs/scripts/action.go`:
  - `StreamContent` — `GraceDurationSec`, `GraceFreeRateMbps` fields surfaced to template/JS
  - `streamContent` — `applyGraceRules` invoked once at the top, before any rest-api call
  - Step 3 bandwidth check — cached/rate-limit branch gated on `!graceMode`
- `templates/views/action/stream_video.html` + `stream_audio.html` — `data-grace-duration-sec` attribute on player tag; `#grace-cta` popup on video page (title + body + `Get 50 Mbps` primary + `Continue at <rate> Mbps` secondary + dismiss X)
- `templates/views/action/errors/slow_download.html` — simplified: rate-limited branch removed, BT-slow only
- `assets/src/js/lib/player/Player.jsx` — `useEffect` toggles `#grace-cta` visibility when `state.currentTime` (movie-time, includes seek offset) crosses `graceDurationSec`; wires dismiss + continue-slow handlers with Umami
- `locales/{11 langs}.json` — `action.grace.{title,body,continue,continueWithRate,dismiss}` keys

### claims-provider, rest-api

No changes. claims-provider stays a read-only tier-info source. rest-api signs the same export URLs as before — web-ui post-processes.

## Movie-time semantics

`movie_time(N) = session_offset + Σ EXTINF_0..N-1` — the start time of segment N inside the source video. THP includes a segment in grace iff its **start** time is below `duration_sec`. Segments straddling the boundary get grace (bounded extra ≈ one segment duration).

Session offset is quantized to 30s by the transcoder seek-quantum (`Session.Start`), so multiple users seeking nearby positions share one FFmpeg run.

## Anti-fraud

| Vector | Defense |
|---|---|
| Walltime abuse (pause + come back) | Movie-time enforcement → pause irrelevant |
| Reload to reset grace | Movie-time bound — re-watching the first 20 min is the only "win"; advancing requires losing progress |
| Replay primary token on other content | Primary claims carry `hash` whenever Rules is set. THP's generic `claims["hash"] != src.InfoHash → 403` check fires on the manifest request itself |
| Replay grace token on other content | Same `hash` claim on the inner grace token — same THP check fires on segment requests |
| Tamper with rules in URL | Rules live inside signed primary JWT; tampering invalidates signature |
| Forge new grace token | Requires `WEBTOR_API_SECRET`; only signing services have it |

## Configuration

Web-ui flags (CLI / env):

| Flag | Env | Default | Notes |
|---|---|---|---|
| `--grace-rules-enabled` | `GRACE_RULES_ENABLED` | false | Master kill switch |
| `--grace-duration-sec` | `GRACE_DURATION_SEC` | 1200 | Grace window in movie-time seconds |
| `--grace-rate` | `GRACE_RATE` | `50M` | Token rate inside grace |

THP picks up the rules automatically — no flag, behaviour is gated by presence of rules in the claim. With flag OFF in web-ui, no rules ever reach THP and the rewriter short-circuits on `findGraceRule == nil`.

## Rollout

1. Deploy content-transcoder. New tag is harmless to existing clients.
2. Deploy THP. With no rules in claims, `modifyResponse` is a no-op for `.m3u8`.
3. Deploy web-ui with `GRACE_RULES_ENABLED=false`. Cap-modal simplification ships in same PR but is gated by `!graceMode`, so flag-OFF behaviour matches today.
4. Flip flag ON. Watch:
   - Bandwidth metrics (expect +50–70 TB/day)
   - Buffering events for free in first 20 min (expect drop)
   - `grace-soft-cta-shown` / `grace-soft-cta-click` / `donate-grace` Umami events
   - Free → paid conversion

Kill switch: flip flag OFF. No DB migration, instant rollback.

## Metrics

| Event | Source | When |
|---|---|---|
| `grace-soft-cta-shown` | client (Player.jsx) | First time `state.currentTime` ≥ `graceDurationSec` |
| `grace-soft-cta-click` | client | Dismiss X or "Continue at slow speed" — `action: dismiss\|continue` |
| `donate-grace` | client (popup link) | "Get 50 Mbps" button click — `tier: free\|anon` |
| `slow-download-shown` | client (existing) | After Sprint 2 only fires for BT-slow — interpret accordingly |
