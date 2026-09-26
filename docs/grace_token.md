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
- **Grace token** (new) — separate signed JWT issued by web-ui per (user, torrent_hash). Carries `kind=grace`, `hash`, `rate=50M`, and since 2026-09-26 the primary token's `sessionID`, `domain` and `exp` (see "Session"). Travels inside the primary token's `rules` claim.

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
  "kind": "grace",
  "sessionID": "<the primary token's sessionID>",
  "domain": "<the primary token's domain>",
  "exp": <the primary token's exp>
}
```

## Session (2026-09-26)

The grace token carries the primary token's `sessionID` and `domain`, named
exactly as the primary names them (`api.NewGraceClaims`, called by
`applyGraceRules`). Before this it carried neither, and thp treated a grace
segment as nobody's: no bandwidth limiter (the limiter needs `rate` and
`sessionID`), and nothing in the viewer's `/session-stats` — the transfer
status was blind inside the window (it drew a view "without the reading"
there, `View.Grace` / `transferStatus.unmetered`, now removed).

With them, on a thp that has the 2026-09-25/26 changes (torrent-http-proxy
`hybrid_bucket.go` `bucketKey`, `session_stats.go` `acquireGrace`):

- **Limited at 50M per (session, rate).** thp keeps one token bucket per
  `bw:limit:<sessionID>:<rate in bytes/s>`, so a session's grace segments
  (50M) and its tier's requests (playlist polls, post-grace segments, 5M)
  draw on two buckets, not one. On a thp keyed by session alone (up to
  sha-277778d) the two would share one Redis balance and clamp it to each
  other's capacity: grace starved by the tier or the tier topped up by grace.
- **Counted, but not as the tier.** `/session-stats` counts a grace segment's
  bytes, `conns` and `active` under the viewer's (sessionID, domain,
  infohash) key — "Вы" is drawn inside the window, an anonymous viewer's too
  — but not its limiter's wait (`throttled`) or its rate (`rate` stays the
  tier's). So inside the window `bytes_per_sec` can exceed the tier's rate
  (up to 10× a 5M cap) with `throttled` near 0, and the transfer status's
  plan verdict stays off: grace is not the tier binding, and nothing is sold
  (`statusview.TestMeter_GraceBytesAreNotTheCap`). The verdict can come on in
  the window's last ~30 s, from the tier's segments hls.js fetches ahead past
  the window; the page sells nothing while its player is inside the window by
  movie time (`lib/playerActivity.js` `inGrace`, `data-run-offset`).
- **`domain` too.** Without it an embed's grace bytes would land on thp's
  `default` key, not the embed's.
- **And `exp`, the primary's.** A token that names a session must not outlive
  it: before this the grace token had no expiry at all, so a segment URL
  copied out of a playlist (an HLS downloader, a pasted link) played any path
  of the torrent at 50M for good — and with the session on it, charged the
  viewer's grace bucket, their `SessionLimiter` caps (past 5 IPs their own
  requests for the file get 429 `ips`) and their `/session-stats` ("Вы"
  drawing someone else's bytes) for as long as anyone used it. thp's JWT
  parser (dgrijalva jwt-go v3) checks `exp` whenever it is present. No
  playback needs the grace token past the primary's expiry (`REST_API_EXPIRE`,
  1 day): thp swaps it in only while serving a playlist on a valid primary,
  and every post-grace segment and playlist poll needs the primary anyway.
  With `exp` the grace token meets thp's `/session-stats` token rules (hash,
  exp, sessionID), which the primary carrying it already does: nothing new can
  read those stats (`api.TestNewGraceClaims_ExpiresWithThePrimary`).

What else reads a grace token's `sessionID` in thp (audited 2026-09-26 against
torrent-http-proxy at 277778d plus its uncommitted limiter/stats change):

| Use | Effect of the grace token now carrying it |
|---|---|
| `HybridBucketPool.Get` (limiter) | Intended: grace segments limited at 50M in their own bucket |
| `sessionStatsWriter` / `acquireGrace` | Intended: bytes, conns, active counted; rate and throttled not |
| `SessionLimiter.Acquire` (prod: 10 per path, 5 big files per hash, 30 per session, 5 IPs per path per 60 s) | Grace segments now count toward the session's concurrency caps, like the post-grace segments on the primary token always have. HLS segments share one per-path slot (the source file's path). Loki, 24 h to 2026-09-26: 26 `session limiter rejected` on `~hls` requests out of 1,180,653 served, 0 for `ips`. A viewer fetches one video and one audio segment at a time: no expected effect. Where a sessionID is shared — a registered embed domain's visitors all carry the owner's sessionID and claims, and a free-tier owner's visitors get grace — their grace segments now share one 50M bucket and the 5-IP cap, as their post-grace segments already did |
| `enforceSessionIP` | None: off in prod, and a grace token carries no `remoteAddress` |
| request log `session_id`, ClickHouse `session_id` | Grace segments now name the session in thp's log lines (Loki); ClickHouse is not configured in prod (empty DSN) |
| `X-Session-ID` to the upstream (content-transcoder, seeder, nginx-vod, srt2vtt, video-info, archiver; also re-sent by `retryTransport`) | None: no upstream reads it (grepped every webtor repo); the transcoder's session id is its own, in the URL |
| Routing (`Resolver`, `ServiceLocation`) | None: by `role` only |

Deploy order: **thp (per-(session, rate) limiter key + grace-aware
`/session-stats`) BEFORE this web-ui.** web-ui first would put sessionIDs on
grace tokens that an old thp keys by session alone — grace and tier sharing a
balance, grace waits read as the tier binding (a plan box inside grace), the
stats' `rate` flipping between 5M and 50M. Never roll thp back past that
change without first rolling web-ui back to grace tokens without a session.
Tokens minted before the web-ui rollout (stream renders cached for 10 min,
open pages) keep grace segments uncounted until they are re-minted; those
pages' old script still has `unmetered`, which does nothing without
`view.grace`.

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
- `services/api/grace.go` — `Rule`, `GraceClaims`, `NewGraceClaims` (the grace claims with the primary's `sessionID` and `domain`), `SignClaims` (signs an arbitrary `jwt.Claims` payload; used to mint the inner grace token)
- `jobs/jobs.go` — three CLI flags (`GRACE_RULES_ENABLED`, `GRACE_DURATION_SEC`, `GRACE_RATE`) wired into `GraceSettings`
- `jobs/scripts/grace.go` — `GraceSettings`, `isFreeTier(c)`, `applyGraceRules(sc, hash, c)`: signs the inner grace token (bound to the viewer's session and domain) and sets `c.ApiClaims.Rules` BEFORE the export call, plus surfaces `GraceDurationSec`/`GraceFreeRateMbps` for the template/JS
- `jobs/scripts/action.go`:
  - `StreamContent` — `GraceDurationSec`, `GraceFreeRateMbps` fields surfaced to template/JS
  - `streamContent` — `applyGraceRules` invoked once at the top, before any rest-api call
  - Step 3 bandwidth check — cached/rate-limit branch gated on `!graceMode`
- `templates/views/action/stream_video.html` + `stream_audio.html` — `data-grace-duration-sec` attribute on player tag; `#grace-cta` popup on video page — a dialog centred over the paused player, the frame dimmed behind it; sized by the player (`@container`): below 32rem of player width it drops the paragraph and takes small buttons so both answers fit a phone's ~200 px 16:9 (2026-09-26) (title + body + the promo plan's CTA — `offer.keepFullSpeed` with `offer.trialNote` under it, rendered only when there is a catalog to sell from — + `Continue at <rate> Mbps` secondary + dismiss X)
- `templates/views/action/errors/slow_download.html` — simplified: rate-limited branch removed, BT-slow only. Since 2026-09-26 the BT-slow check never reads the swarm's speed as the cap (`buildSlowDownloadData`): a free viewer under grace no longer gets "you have ~5 Mbps" and a trial button upfront when the swarm is what falls short — 674 such modals a week before, 4 trial clicks from them. The popup at the end of the grace window stays the stream's first offer, always.
- `assets/src/js/lib/player/Player.jsx` — `useEffect` toggles `#grace-cta` visibility when `state.currentTime` (movie-time, includes seek offset) crosses `graceDurationSec`; wires dismiss + continue-slow handlers with Umami. **The popup stops the film** until the viewer answers it (`player/grace-hold.js`, see "The popup holds the film" below). It also marks the `<video>` for the resource page's transfer status (`lib/playerActivity.js`): `data-grace-cta-shown` once the popup is up, `data-grace-cta-hold` while it holds playback, `data-grace-cta-answered="continue|dismiss"` when the viewer answers it (the trial link is no answer: it opens a new tab and the popup stays). See "The popup and the transfer status's plan box" below
- `locales/{11 langs}.json` — `action.grace.{title,body,continue,continueWithRate,dismiss}` keys

### The popup holds the film

Owner, 2026-09-26 ("я бы всё-таки останавливал видео при появлении попапа"):
the film stops when the popup comes up and goes on with the answer. Before,
it played on under the popup, and the minutes past the free window went by at
the cap while the viewer read the offer. `player/grace-hold.js`, driven by
Player.jsx:

- **Up** — a playing film is paused (fullscreen is left first, as before: the popup lives outside the fullscreen element). hls.js keeps filling the buffer at the cap meanwhile — intended: the answered film then plays from a longer buffer.
- **Anything else that starts it behind the popup** — the element's own `autoplay` when a session seek's new run becomes playable (hls.js reloads the element and the reload re-arms it; seen in Chrome on 2026-09-26, paused back in the same millisecond), the subtitle catch-up letting go, the embed's `player_play`, a local seek with `play` — is paused back on its `play` event and counted as playback wanted.
- **A session seek past the window** puts the popup up as it starts (the timeline shows the target at once). The seek has paused the film itself; before it starts the new run it asks the hold (`session-seek.js` `holdPlayback`) and, held, loads the run without playing it and lets go at `canplay` — paused under the popup, seeking unlocked. A refused seek does not restart the old run behind it either. A seek that lands paused never asks.
- **The answer** — "Continue at N Mbps" or the close: the film goes on **only if the popup held playback**. A viewer who had paused before it came up, or a seek that lands paused, stays paused. **Play while the popup is up** (space/`k`, the big button, a click on the picture, the headset's play) **is the answer "continue"**: the popup closes, the element is marked `continue`, and the film plays whoever paused it — Play that did nothing would read as a broken player. Not blocked.
- **The trial link** — no answer: a new tab, the popup stays, nothing resumes behind it.
- **The player goes** (the next file, a teardown) **or the viewer presses Next** while the popup is up — the hold is dropped without resuming: an answer given while the next file loads does not start the one being left. The next file is a new element with its own window.
- **The transfer status** does not read the popup's pause as the viewer's: while it holds playback the element carries `data-grace-cta-hold`, and `lib/playerActivity.js` counts it as playing — the viewer stays on the chain (`streaming`) and the verdict keeps no minute (docs/vault.md "Your speed and the plan limit"). Paused by the viewer before the popup came up — no mark, the ordinary pause rules.

### The popup and the transfer status's plan box

One offer at a time (owner, 2026-09-26). The resource page's transfer status
(`lib/transferStatus.js` `present`, docs/vault.md "Your speed and the plan
limit", docs/transfer_status.html) has its own plan box at the cap, and past
the window thp binds on the very next segments, so the server says the box is
due as the popup goes up. The sequence the viewer gets:

1. **Inside the window** (movie time) — nothing is sold by the status, whatever the server's verdict (hls.js fetches the segments past the window ahead, at the cap).
2. **The element crossed, the popup not up yet** (a frame or more; none while the tab is hidden) and **the popup up** — the popup is the offer; the status says the cap as a line, no box (`playerActivity.graceOfferDue`, `data-upsell-surface="grace"`).
3. **Answered** — "Continue at N Mbps" or the close: the viewer has just been told the cap is coming. For a file known to be over the cap (`data-status-over-cap`) the status shows no box and no line (the pink link still says the cap) until the player's **first real stall** (`playerActivity.offerAnswered`: a `waiting` lasting 1.5 s after it played; one under way at the answer counts, one that ended before it does not). Before 2026-09-26 the box came ~0.5 s after the popup closed.
4. **The first real stall** — the stream box, as at any capped stall (the server's timing unchanged: the pink link after 3 events at the cap, the box after 8 s, gone 10 s after the cap). From then on the file is sold as any file over the cap: the box while it plays, once due.

Files of unknown bitrate or under the cap keep their rules (no box while they play, the box at a real stall). Without a popup (paid tiers, `GRACE_RULES_ENABLED=false`, the audio page) there is no answer and nothing changes. The marks live on the `<video>`: the next file, a reload or another grace window starts without them; a session seek keeps them.

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
| Grace token copied out of a playlist | Expires with the primary token that carried it (`exp`, 1 day): the session it names is charged only as long as the viewer's own token lives |
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
| `grace-soft-cta-shown` | client (Player.jsx) | First time `state.currentTime` ≥ `graceDurationSec` — `paused: true` when the popup stopped a playing film (since 2026-09-26; `false` for a film already paused — by the viewer, or by a session seek through hls.js, whose held run shows on the click) |
| `grace-soft-cta-click` | client | Dismiss X or "Continue at slow speed" — `action: dismiss\|continue`; `via: button\|play` (Play while the popup is up counts as `continue`); `paused` — the popup held playback and this answer resumed it |
| `donate-grace` | client (popup link) | promo-plan CTA click — `tier: free\|anon`, `target: trial\|checkout\|donate` |
| `slow-download-shown` | client (existing) | After Sprint 2 only fires for BT-slow — interpret accordingly |
