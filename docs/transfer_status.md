# Transfer status as the user sees it (2026-09)

The resource page's transfer status — the chain "Swarm ▸ Cache ▸ You" while
something moves, the old badge while nothing does, the piece bar, the hint,
the plan box and the details popover — and the Vault states it draws.
Moved out of `docs/vault.md` on 2026-10-03; Vault's own side (pledges, the
dashboard and its live rows, which draw the same badge) stays there. The
design: `docs/transfer_status.approved.html` (as approved, frozen) and
`docs/transfer_status.html` (every state drawn by the real classes).

`handlers/resource/status.go` used to collapse queued / storing / failed into
one "Vaulting N%" badge. Two states are now their own. The resource page
draws them as the old badge while nothing moves and in the transfer chain
while something does (the Vault node, the hint under the piece bar, the
details popover — "Chain or badge" and "Your speed and the plan limit"
below); the Vault dashboard's rows show the state's label:

| State | When | Resource page |
|---|---|---|
| `vault_waiting` | funded, nothing stored yet, and the page's status stream has had live replies (the seeder in the swarm) for at least 30 s (`noSeedersAfter`; a page open or a stats reconnect starts the window over, as for the caching badge) and they show zero seeders and zero peers — not from a cold reply (nobody streams the torrent and Vault has not started on it, e.g. the pledge still queued: `vaulting`, "Saving 0%"), not in the first 30 s of live replies (a torrent just loaded has an empty swarm while the seeder reaches trackers and the DHT), not for content whole in the cache (Vault takes it from there: `vaulting`, "Saving 0%") | Badge "Waiting for seeders" (purple, clock), the purple piece bar of what the cache holds (as approved, 2026-09-25); hint: Vault keeps checking and returns the points if none appear. With the viewer on the chain it is "Cache ▸ You" — the cache (its share of the torrent, "43%", or the check once all of it is there) is where their bytes come from, Vault has stored nothing; details "what we have downloaded so far". Nothing moving: the Vault node, details "not stored yet" |
| `vault_missing` (view key) | Vault's transfer, nothing moving, peers there but no seeder, and pieces nobody connected has (the seeder's availability, below) | Badge "Waiting for missing pieces · 58%" (purple, clock), the holes hatched in the bar; hint: the peers on hand have only N% of the torrent, Vault keeps checking and returns the points if the pieces do not appear |
| `vault_failed` | Vault API `status=3` | Badge "Transfer failed, retrying N%" (amber); hint: the last attempt did not go through, nothing is lost. With the viewer on the chain it is "Cache ▸ You" (the cache serves them, as for `vault_waiting`); nothing moving: the Vault node amber, "retrying", details "not stored yet". The API's `Error` text is **not** passed on: it is the Vault worker's raw error, which quotes the URL it fetched (token and api-key included), and nothing shows it |

Everything else is unchanged: queued/processing render as `vaulting` (drawn
`vaulting` with the viewer on the chain, `vaulting_only` — "Swarm ▸ Vault" —
without, the old "Saving N%" badge as `vaulting_idle` while nothing moves),
the 7-day transfer timeout (`VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD`) still ends
in the reaper's letter. Review with `?debug_status=vault_waiting` /
`?debug_status=vault_failed` on any resource page (dev-only).

## Chain or badge (2026-09-25)

The chain is drawn only while something moves — the swarm sends to the
cache (caching, or into Vault) at a rate its label shows, or a request of
the viewer's is open (bytes going to them, a stall, a piece nobody has, or
no number yet). It draws only who takes part: only caching — "Swarm ▸
Cache"; from the cache or Vault — "Cache ▸ You" / "Vault ▸ You"; both — the
whole chain; the source node is always on it, and a viewer thp says nothing
about is never drawn. The middle node is **the cache**, not the brand
(owner, 2026-09-25; the approved design still reads "Webtor"): its share of
the torrent while caching ("Cache 43%"), the green check once all of it is
there (no word next to it; the details' row says "100%"), and while the
torrent goes into Vault it is Vault's node with Vault's share ("Vault 64%",
the layers icon) — with the viewer downloading too, not "Webtor → Vault";
its row in the details says "what Vault has stored so far"
(`details.vaultSavingSub`), never "a stored copy — no seeders needed" while
the swarm still sends the rest (that one is for `vaulted` only). Nothing
cached yet (`idle_torrent`) with a request of the viewer's open: "Cache ▸
You" with no word next to the cache — "waiting" there read as nobody being
around, right next to "You", and a "0%" is not backed once the seeder's
stats are gone (an unloaded torrent is idle too). A wait for data there (a
request open, nothing for 5 s) is the swarm's: before the first verified
piece nothing cached could keep the viewer waiting, so it is `stalled` —
"Swarm ▸ Cache ▸ You" and the stall's hint, as one piece in (a slow swarm's
first 8 MiB piece at 1.2 Mbps takes ~53 s; `TestBuild_IdleWaitIsTheSwarms`).
Phone caption "cache"; `Node.Kind` `cache`. When nothing moves the page shows the badge it had before the chain
(git 5d55b26e: "Caching paused 43% (14 seeders)", "No seeders · 43%", "In
cache"…; two as the approved design draws them: "checking" cyan, "In Vault"
purple with Vault's layers), with the piece bar and the state's hint under
it. The sticky bar and the details popover exist only with the chain; the
plan box only with the chain at the cap. A press on the chain is Umami's
`status-details` (2026-10-03; no props): whether anyone opens the details,
where the ≈ MB/s and the plan's line are. The chain toggles the popover, so
the rarer press that closes it counts too.

`statusview.View.Mode` is `chain` or `badge`, and the key (the cause) is
separate from it: the approved rows are `active`, `tier` (→ `tier_dl` /
`stream_ok` / `stream_stall` on the page; `stream_over` added 2026-09-26),
`swarm`, `stalled`, `missing`,
`caching_only`, `cached_flow`, `cached_tier`, `vaulting`, `vaulting_only`,
`vaulted`, `vaulted_tier` (chain) and `checking`, `paused`, `noseed`,
`missing_idle`, `idle_torrent`, `cached`, `status_unknown`, `vaulted_idle`,
`vault_waiting`, `vault_missing`, `vault_failed` (badge); `caching_idle` and
`vaulting_idle` (caching or saving, nothing measurable moving, no verdict —
a reconnecting stats stream, pieces asked for and not arriving) were added
after the approval.

**Transitions.** The two participants leave differently. **The swarm** is
held: `statusview.Hold` — one per status stream (`viewEnv.hold`) — keeps it
on the chain for `HoldFor` (10 s) after it last moved — or, once the gap
between its last two moves is known and longer, for half as long again as
that gap, at most a minute (`maxHold`): a slow swarm's big pieces (1.2 Mbps
verifies a 4 MiB piece every ~27 s) handed the chain to the badge for the
rest of every gap, 23 switches in 300 s (46 for 2 MiB, 11 for 8 MiB), and
now only its first gap, before the second piece says how long one is, shows
the badge (`TestHold_SlowSwarmStaysOnTheChain`; a stop after such a swarm
reaches the badge that much later). The gap is measured from a still call
to the next move: the loop calls the hold more than once on one piece (its
frame, a tick, a thp event within `movingFor`), and the second call of a
piece measured "a gap" of a fraction of a second -- back to `HoldFor`. A
swarm that moves in every call for longer than `HoldFor` has no gap any
more: a fast one's frame comes every second and the tick within
`movingFor` of it, so no call saw it still, and the gap from its last pause
(the player's, a seek, a cold start) stayed -- after 40 s of nothing and
five minutes of moving, its stop kept the chain for a minute
(`TestHold_AFastSwarmForgetsAnOldGap`). Through the hold it is **drawn as it last
moved**: its last speed, the sweep, and the key that went with it
(`Input.HeldBps`) — never a pause, a dash or a badge key's hint on the chain;
the paused, missing and idle stories are the badge's, told once the hold is
over. The swarm *moves* while its pieces arrive: the loop gives the view a
rate only within `movingFor` (0.5 s) of Completed growing
(`TorrentStatus.swarmStill`). Judged by the smoothed rate instead, a 38 Mbps
swarm "moved" for 13 s after its last byte (the rate decays by 0.6 a tick)
and the hold came on top: the badge 14–22 s after the last byte, the last
ten of them a chain with "paused" or "— Mbps"; now it is ~11 s
(`TestStatusStream_BadgeTenSecondsAfterTheLastPiece`). The smoothed rate
itself no longer goes out on the status JSON (below).

**The viewer** is on the chain by thp's word on their requests, never by
their speed (owner, 2026-09-25): while thp saw a request of theirs for the
torrent open since its previous event (`active`, or `conns > 0`), and until
thp has seen none for `statusview.PresenceDebounce` (2 s) —
`Viewer.Present`, `Sample.presence`, `statusview.Meter`. Speed is only the
number on their segment (a dash while a request is open and no bytes came
yet, and again once none came for 10 s — `freshAfter`: the last number
stands through a second without bytes, not for good; thp counts a request
from its first 2xx byte, so a segment the transcoder holds back reads as
this dash, not as a stall; `TestMeter_LabelGoesWithTheBytes`). Measured the same day: thp's `conns` went 1→0 within 1.2 s of a
client's abort, and the page drew "you" for ~25 s more — the five-second
window's tail, the meter's 10 s speed hold, a chain hold on top; the speed
hold is gone. When the viewer leaves and nothing else moves, the badge comes
at once: 2–3 s after thp saw the request close (itself within 1.2 s of the
client's abort, measured above;
`TestStatusStream_ViewerLeavesWithTheirConnection`); a swarm that moved keeps
its own hold. The debounce is counted on thp's events (one a second), not
the wall clock between them (`Sample.quietEvents`): an event that carries
`active` covers the whole second before it, so the second one in a row
without a request ends it; with `conns` alone (a thp from before the field)
the request last seen open may have lasted until just before the next
sample, so it takes a third. Either way **a gap shorter than 2 s never
drops the viewer** (a download manager swapping one range request for the
next) and **one of 3 s or more always does**, 2–3 s after their last
request closed, whatever bytes the window still carries
(`TestMeter_DebounceIsTheViewersTwoSeconds`,
`TestStatusStream_ConnectionGapsUnderTheDebounce`). Counting `active` the
point sample's way — the third event — cost every departure a second
(review, 2026-09-25): thp's first event after a request closes still says
`active` (its `ends` counter moved), so the run of events without one
started an event later, and an abort reached the badge 3–4 s after the
close.

**Why `active` and not `conns` alone** (live, 2026-09-25): `conns` is a
point sample — the requests open at the moment thp samples, once a second.
A request that opens and closes between two samples is never in it, and a
paid viewer's page player (no limiter) fetched each HLS segment from the
transcoder in well under a second: thp read 1.1–2.2 MB/s and `conns` 0 in
*every* event, "Вы" never came, and the page's player had no last reading to
keep them by (`Meter.Last` is recorded only while they are present). thp's
event now carries `active`: true when a content request of the key was open
at any moment since *that stream's* previous event — those that opened and
closed between two events included; tracked per stream, so two tabs on one
key do not take each other's answer. `true`/`false` is thp's word, the bytes
notwithstanding (`TestStatusStream_PagePlayerHLSViewerIsOnTheChain`,
`TestMeter_PresenceIsARequestSinceTheLastEvent`). **A thp from before the
field** (no `active` in the event) falls back to `conns > 0` or
`bytes_per_sec > 0`: the HLS player is seen, at the price of a tail — the
viewer stays until thp's window (5 s) has emptied, then the debounce
(`TestMeter_OldThpFallsBackToTheBytes`). The plan's verdict turns on only
while requests come, by the same `active || conns`. A **stall** stays on
`conns` — a request open *now* with nothing arriving: a request the node
holds waiting for a piece is open at every sample, while quick requests that
each bring almost nothing (hls.js reloading a playlist the transcoder is
still growing) are not a wait (`TestMeter_StallIsARequestOpenNow`).

**Known, not fixed in web-ui: a paused player's playlist reloads read as the
viewer** (review, 2026-09-25; source-checked, not measured in a browser).
The transcoder leaves `#EXT-X-ENDLIST` off its playlist until its run
completes, and its pacing stops FFmpeg once it is well ahead of a paused
viewer, so the run does not complete while they are paused. hls.js 1.6.14
treats such a playlist as live and reloads it every target duration — half
that while it comes back unchanged: about every 2 s for the transcoder's
4 s segments, the audio rendition's too — paused or not (web-ui stops
loading only on destroy and on a session seek). thp counts every 2xx that
is not an event stream, playlists included, and nothing in its event tells
a playlist from a segment. So a paused viewer with a full buffer reads
**present**: with `active`, on the chain for as long as they stay paused
(reloads further apart than the debounce would blink it instead); with an
older thp, the reloads keep the window's bytes above zero, the same. The
fix is thp's: leave playlist responses out of `conns`, `ends` and ideally
the bytes. `TestMeter_PlaylistReloadsKeepAPausedViewer` pins what the meter
makes of them until then.

Otherwise an HLS player honestly has no request open once its buffer is
full, often for far longer than the debounce — so the page's own player
decides there: the server cannot see it, and for a viewer who reads gone it sends, next to the view, the whole view
with them on the chain at their last reading (`view.playing`,
`Meter.Last`: the number and the plan's verdict as they last were, never a
wait). The block draws that one while its player *plays*
(`playerActivity.phase` `playing`: not paused by the viewer, or held by the
grace popup: the popup stops the film until the viewer answers it, a pause
of the page's and not theirs, and the player marks the element
`data-grace-cta-hold` while it holds playback; read as playing, for the
chain and the verdict alike, docs/grace_token.md "The popup holds the
film"), in place, the same nodes (`transferStatus.playing`). Not while it is
stalled for real (`buffering`, 1.5 s and more): the viewer waits ("waiting
for data") — the number froze with the last segment. Paused by the viewer,
on the chain with "pause" (2026-10-02). The player's label and cause read
the server's verdict the same way while it *streams*
(`playerActivity.streaming`: playing or waiting for data now, or paused and
its buffer still growing). "Still buffering"
is measured from the buffer as it was when the player paused: while it
plays the baseline follows its buffer on every sample
(`playerActivity.sampleBuffers`); baselined only while paused, it compared
with the buffer before the film started, and every pause kept the viewer
on the chain for `BUFFER_IDLE_MS` (10 s). Inside the free grace window too:
grace segments are the viewer's requests like any other (below).

**The plan's cap** has transitions of its own ("Your speed and the plan
limit" below): the pink link after 3 of thp's events at the cap over its
whole five-second window, off at the verdict's first miss; the plan box after
8 s of a steady cap, and sent until 10 s after the cap was last seen — through
a dip it stays, so the ~80 px box does not come and go with the bucket's
sawtooth or a slow second of the swarm, and a small file's few seconds at the
cap show the pink link and no box. **Once up on the page, the box stays**
until the viewer closes it (owner, 2026-09-27; `lib/transferStatus.js`
`keepBox`, below): the server's sending it or not decides only when it comes
and what it says. Neither comes up on the first seconds of a
new stream to thp (every token rotation, every reopen, a web-ui rollout
reconnecting every page), whose events average over the 1–4 s thp's ring
holds so far and read a player's segment fetched at the cap as the cap; nor
does the box rise on the window's tail after the viewer's requests closed.
The viewer leaving takes the pink link at once (no viewer on the chain, no
plan) and the server's box with it; the page keeps the box it drew.

The viewer is also held through a lost stream to thp (`Hold.Viewer`,
`sessionWatch.reconnecting`) — a gap in the data, not a departure: a pod
rotation or an ingress reload cuts every stream on a node, and the reopened
stream's first event carries no speed — without the hold, cached content
being watched blinked to the "In cache" badge and back. Only while a reopen
is on its way, for `HoldFor`: a final answer, spent retries or an open
stream gone silent leave the viewer unknown. A reopen starts the meter over
(`Meter.StreamOpened`) but keeps the last reading on the chain (`Meter.Last`):
the page's player playing across it, whose first counted samples on the new
stream fall between two segments, keeps the viewer there by it — it used to
read the badge until a sample caught a segment request open
(`TestViewEnv_PagePlayerKeepsTheViewerAcrossAReopenInAnHLSGap`). The badge turns into the chain
at once. The loop ticks every second, so the second a hold runs out does
send the badge. Both are in
the block for good (`.tx-head`, one grid cell; the one not shown is only
invisible), so the row is as tall either way (24 px) and a switch never
moves the card; focus on the chain or in its details moves to the badge
(`tabindex=-1`) and back to the chain when it returns, and the chain's
accessible description is the badge's words (`aria-describedby`).

## Pieces nobody has (seeder availability, 2026-09-25)

torrent-web-seeder's stats frames carry the swarm's availability over the
same pieces (`api.EventData`: `availability`, `availability_known`,
`missing` — sorted half-open runs of positions, merged past 512 —
`missing_unchanged`, `wanted_missing`, `reader_missing`). The stream is
stateful for `missing` like for the pieces: a frame with `missing_unchanged`
keeps the runs, any other frame replaces them (null or [] — none).
`pieceMap.holes` folds the runs into the bar's 256 cells the way the pieces
are folded, counting only pieces not complete here (a merged run may cover
complete ones, and completion is drawn over the hatch), and sends the
bitset as `missing` next to `pieces`/`active`. Nothing of it counts until
`availability_known`: before the peers' bitfields are in, the union paints
holes that are not there. A seeder without the fields (every one deployed
before them) reads as not known: no hatch, no missing states.

With it the bar hatches what nobody connected has (a red 135° hatch, over
the pulse), in either mode — one layer under the whole bar, masked to the
missing cells (`.tx-pbar::before`, `--tx-holes` from `paintBar`): painted
per cell, the stripes started over in every cell and the page's 256 cells
read as a row of blocks. Three states say why: the viewer waiting on such a
piece (`reader_missing > 0`) with peers but no seeder — `missing`, on the
chain ("0 Mbps · needed pieces missing" on the swarm's segment); nothing
moving, pieces wanted or missing, peers but no seeder — the `missing_idle`
badge "Needed pieces missing · 43% (12 peers, 0 seeders)"; the same for
Vault's transfer — `vault_missing`. The first two also before a single piece
is cached (`idle` carries the swarm and its availability: the head of a file
nobody has keeps a torrent at 0% for good), and none of the three in the
first seconds of a status stream before the swarm has had the time to show
it moves (`Torrent.Settling`: `settleAfter`, or until it first moves). The
percent and the holes are the whole torrent's — every file, not only the
page's: the stream is asked for the torrent's stat, and so the hints say
"of the torrent" ("в рое есть 73% раздачи"), never "of the file" (owner,
2026-09-25). They say the cause only ("12 peers are connected, but every
copy is incomplete: the swarm has 73% of the torrent…") and, for the first
two and only where Vault is
configured, end with the page's own Vault link — the pledge form for a
signed-in viewer, the login first otherwise (`data-umami-event`
`vault-clicked` / `vault-clicked-anonymous`, `location=status`).

The first frame of a stream lists every piece: ~405 KB at 8k pieces, ~3.2 MB
at 64k. `api.Stats` reads lines up to 16 MB (`statsMaxLine`) — at the old
1 MB cap a torrent of ~20k pieces or more never got a status at all. It
keeps a 4 KB read buffer for the stream's life (`statsLines`): a longer line
is put together in a slice of its own and dropped once decoded. A
`bufio.Scanner` kept its grown buffer for as long as the page stayed open —
~4 MB a stream at 64k pieces, on the pods whose 2Gi limit `Stats` buffers
already pushed into OOM once (`docs/warmup.md`); `TestStats_BigFrameIsNotKeptForTheStream`
holds a stream to ~32 KB after a 3.2 MB first frame. A line over the cap
costs that frame, not the stream: the frames after it carry the totals and
the pieces as diffs.
Review: `?debug_status=caching&progress=43&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5&viewer=zero&debug_pieces=stream`
(add `reader_missing=3&viewer_stalled=1` for `missing`).

## Piece bar

Under the chain or the badge the resource page paints the picture torrent clients do: 256
cells, each the share of its pieces the seeder holds, pulsing where it is
fetching. Data comes bucketed from the seeder's per-piece stats on the same
status SSE (`handlers/resource/status.go` `bucketPieces`, ~350 bytes per
update); no extra seeder wake-ups — the status SSE already opens the stats
connection. It is deliberately the whole torrent, not per file, and only on
the resource page, and only while something moves (`caching`, `vaulting`,
`vault_failed` with stored pieces). Complete content (cached, vaulted), idle,
unknown and waiting states show a hairline divider in the same 6px slot, so
the header never jumps when a transfer starts or the seeder's stats channel
closes. Review: `?debug_status=caching&debug_pieces=stream` (also
`sparse`, `half`, `full`, `empty`).

## Caching: checking → caching / paused / no seeders

Partially cached content is not judged on sight. `judgeSwarm`
(`handlers/resource/status.go`) watches the stream for `settleAfter` (5 s):
progress or a queued piece at any moment → caching (the chain's swarm
segment flows with its speed, the cache node shows "N%"); with no activity the
badge says "Checking activity…" with the dots (`checking`) for those 5 s and
then "Caching paused N%" (amber, pause glyph; the seeder downloads on
demand, so nothing moving means nobody is streaming it), whoever is or is
not around. Only a swarm that stayed empty (no seeders, no peers) for the
whole of `noSeedersAfter` (30 s) of the stats stream's live frames turns it
red: "No seeders · N%" (`noseed`). A stats reconnect starts that window over
(`TorrentStatsData.LiveFor`, as for `vault_waiting`): the pod it lands on
after a seeder restart has an empty swarm at first like any other, and
counting from the first stream's first frame called it "no seeders" six
seconds after the reconnect, taking the plan's card with it. The long window exists because a freshly started seeder pod sees
an empty swarm for tens of seconds while it reaches trackers and the DHT —
16 s to the first peer on a real torrent (2026-09-03) — and an earlier
version kept the spinner for that whole window, which read as stuck. A piece
boundary cannot flicker a live download into "paused". Review:
`?debug_status=caching&progress=40&checking=1|paused=1|noseeders=1`.
The page renders `checking` (statusview `Pending`) and only the stream
moves it on, so without JS it is the status for good: a `<noscript>` style
in `partials/resource/status.html` gives it the still dots of reduced
motion, not the running ones (2026-10-03).

## No seeders / stream reconnect

`caching` with zero seeders and zero peers renders "No seeders · N%" (red,
`noseed`; the swarm node red where a waiting viewer puts it on the chain) —
the swarm is empty, nothing can progress; it wins over "paused". A stats
stream that closes (seeder pods are rotated on every deploy, closing every
stream they held; and the seeder ends every stats stream at 30 minutes,
torrent-web-seeder `Stat.StatStream`) is reopened with jittered backoff
(about 2…32 s, five attempts, `statsWatch`). A stream that lived at least a
minute (`statsLived`) ended as planned: the budget starts over, and it is
reopened whatever the progress, for any torrent not yet whole — the seeder's
stream only peeks and never loads the torrent, so this costs an export and a
stream. It used to be a failure like any other: each half-hour close spent
an attempt that never came back, and only a download with bytes in the last
minute was reopened, so a paused download read "idle" from its first
half-hour close and an active one from its sixth, for as long as the tab
stayed open (`TestStatsWatch_PlannedClosesKeepTheStatus`). A stream that dies
younger is reopened only while something was stored, not all of it, and
bytes moved within the last minute (`shouldReconnect`). The planned close is
logged at Info, an early one at Warn. A
stream that closes with the torrent **complete** is neither: the seeder
closes it once the last piece is in, and the status stays `cached` ("In
cache", the cache node's check) — it used to forget the stats and read
"idle", "Webtor ожидает" (5461f58a…, 2026-09-25;
`TestStatusStream_CompleteTorrentStaysCachedWhenTheStreamCloses`). A pod
going away on a deploy sends one last frame on every stream it holds —
`status: 3` (TERMINATED) with every counter zero — and ends them. That frame
is not stats and is skipped (`api.StatTerminated`): taken for stats it read
"idle", and the close after it was never reconnected (nothing stored, as
far as the loop knew), so the page never learnt the new pod finished the
torrent (`TestStatusStream_SeederRolloutMidDownloadReconnects`). Meanwhile the last
status stays on screen without speed or verdicts (nothing measurable moves:
after the hold, the "Caching N%" badge, `caching_idle`); after the retries
the badge says the status is unavailable (`status_unknown`, with a hint that
this says nothing about the torrent itself). Review: `?debug_status=caching&progress=40&noseeders=1`.

## Your speed and the plan limit (2026-09)

The swarm rate says how fast the seeder fetches; it says nothing about how
fast the viewer gets the bytes. The resource page's status is the chain
"swarm ▸ cache ▸ you" (approved design and every state: docs/transfer_status.html;
markup `partials/resource/status.html`, view `services/statusview`, client
`lib/transferStatus.js`): the viewer's own link is the last segment, and while
the node sends to them at their plan's cap it turns pink and a plan box appears
under the piece bar.

- **Source.** torrent-http-proxy's per-session stream, `GET
  /session-stats/<infohash>?token=…&api-key=…` (one `data: <json>` a second:
  `window_sec` — the span an event averages over once the stream's ring
  holds that much (5 s); a new stream's first events cover 0, 1, … 4 s of it
  —, `bytes_per_sec`, `conns` — the requests open at the sample —, `active` — a
  request open at any moment since this stream's previous event, absent from
  a thp older than the field —, `rate`, and `throttled` — the share of the
  window the limiter held the session's traffic for this torrent, absent when
  no limiter applied). The node that serves the viewer holds the counters, so the
  host is taken from an export URL rest-api returned for the torrent — the
  same export `tryConnectStats` already fetches, now asked with
  `use-premium-domain=false` (the premium edge buffers an event stream): the
  `torrent_client_stat` URL, or for cached content the `download`, then the
  `stream` one (`sessionStatsTarget`, path prefix and api-key kept, the
  export's own token dropped). web-ui calls it server-side (`api.SessionStats`)
  and folds it into the existing `/status` SSE as `view` — the browser never
  talks to thp for this. Only a stream that asks with `session=1` opens one:
  the resource page does, the Vault dashboard's rows (`vault/progress.js`) do
  not — theirs carries the view's `badge` alone, built with no viewer. It is
  opened once the stats connection's result is in (the export names the
  node). The stream's first message waits for the seeder's first frame, or
  for a final answer about the stats stream (cached, failed), up to 3 s
  (`firstStatusWait`): connected without a frame the status is "idle", and a
  partly cached torrent blinked from the page's "checking" to "waiting" and
  on to "caching" (`TestStatusStream_FirstMessageWaitsForTheFirstFrame`).
- **The token** is minted server-side for every open (`sessionStatsToken`):
  the viewer's own claims — the sessionID and domain rest-api signs into the
  page's export links, so thp keys the counters the same way — with `hash` =
  the lower-case infohash, a 10-minute expiry, no grace rules. It never
  reaches the browser (HTML, JS or SSE) and is kept out of transport-error log
  lines. thp takes a stream token only when it is bound to that torrent.
- **What is measured, and what is not.** thp counts what it wrote
  downstream, and downstream is ingress-nginx with proxy buffering (about
  260 MB per response; a paid viewer on the premium domain has the edge's
  larger buffer in front of that), not the viewer. A viewer on a link slower
  than their plan still has the limiter pacing thp until that buffer fills.
  So the chain says what the node sends, and the plan box states the cap as a
  fact ("without a subscription — up to 5 Mbps"), never "your plan limits
  your speed".
- **The reading** (`statusview.Meter`): the first event of a stream has a
  zero-length window and counts as unknown; the viewer is on the chain while
  thp's events see a request of theirs (`active`, or `conns > 0`) and until
  they have seen none for `PresenceDebounce` (2 s: two of thp's events with
  `active`, three without — `Sample.quietEvents`)
  (`Viewer.Present`, "Transitions" above) — the speed never decides, except
  with a thp that sends no `active` (then `bytes_per_sec > 0` counts too); a
  viewer who reads gone is a known absence and nothing of theirs is drawn,
  whatever the window's tail says. While present, the speed is
  smoothed over about 3 s and rounded to its label, so a second that looks
  the same sends no message; through a sample without bytes the last label
  stands; a request open at the sample (`conns`) with no bytes for 5 s is a
  stall; a request open
  before any bytes reads a dash. No event for 3 s makes the reading unknown.
  Nothing open (including "not started yet", and a finished download) keeps
  them off the chain, and no data (no stream, a thp without the route, a
  refused session, a stale stream) never draws them. `Meter.Last` keeps
  their last reading on the chain with a number for the view the page's
  player keeps (`view.playing`).
- **The grace window** (2026-09-26). Grace segment tokens carry the primary
  token's `sessionID` and `domain` (`api.NewGraceClaims`,
  docs/grace_token.md "Session"), so thp counts grace segments in the
  viewer's stream like any other request of theirs: their bytes, `conns` and
  `active` — "Вы" is drawn inside the window, an anonymous viewer's too — but
  not their limiter's wait (`throttled`) or their rate (`rate` stays the
  tier's). Inside the window `bytes_per_sec` reads up to the grace rate (50M,
  ten times a 5M cap) with `throttled` near 0, so the verdict stays off: the
  grace bucket binding is not the tier binding
  (`TestMeter_GraceBytesAreNotTheCap`). It does come on in the window's last
  half a minute or so: hls.js fetches the segments past the window ahead of
  the playhead, on the primary token, at the tier's cap — recorded
  2026-09-26, the box was due 6.7 s before the player left a 30 s window. The
  page sells nothing while its own player is inside the window by movie time
  (`playerActivity.inGrace`: `currentTime` plus the transcoder session's
  offset, `data-run-offset` from Player.jsx — after a session seek the
  element's clock counts from the seek point, and a player 24 minutes in read
  6 s; `present`'s `inGrace`): no box and no selling line, whatever the
  server says. Until 2026-09-26 grace segments carried no session, the
  reading was blind there, and the server sent a view without it
  (`view.grace`, `transferStatus.unmetered`); both are gone.
- **The verdict** (`planLimited`, `planState`) needs two signals, because each
  alone lies somewhere. `throttled ≥ 0.5` keeps a slow swarm out: it never
  makes the limiter wait. But it is measured over the time requests are open,
  so a player fetching a segment every few seconds reads 0.8–0.9 while it
  needs far less than the cap. The second is demand: `bytes_per_sec ≥ 0.9 ×`
  the cap, read from the `rate` claim the way thp's limiter reads it (the
  rate claim's megabit, 2^20 bits: `"5M"` reads exactly 5 Mbps). On only
  while thp says a request of theirs was open since its previous event —
  `active` taken at its word when the event carries it, `conns > 0` only from
  a thp without the field (`Sample.requestedSinceLast`; owner, 2026-09-25) —
  not on the window's tail after the requests stopped; off when
  `throttled < 0.3` or the bytes drop under 0.7 of the cap. **No limiter
  (field absent) — never.** The thresholds against recorded events
  (2026-09-26, prod thp, 5M; `testdata/session-stats-at-cap.json`,
  `meter_replay_test.go`): three capped streams read use p05 0.955–0.978,
  median 0.99–1.00, and once on held it down to 0.742; a file under the cap
  (buffer-fill bursts) read up to 1.016 with `throttled` up to 0.91. Any use
  On in 0.85–0.95 gives the capped streams the same fact and box to the event
  and the file under the cap the fact on one event, never a box; 0.8 gives
  it the fact on 4 events, throttled alone on 21 (it is a sum over parallel
  requests and reads ≥ 0.5 below the cap); 0.92 and up delays one capped
  stream by a second. So 0.9 / 0.7 stay. The box rises, besides, only on
  an event with a request open at thp's sample (`conns > 0`): `active` also
  says yes on the event that sees the last request close, and a box raised
  there went 2 s later with the viewer — a flash
  (`TestPlanState_BoxRisesOnlyWithARequestOpen`); on the recorded capped
  streams every event the box would have risen on had one open. The price of the demand signal: the limiter's
  bucket is per session across all torrents, so two downloads sharing the cap
  each read half of it and neither is called limited. Missing a true case is
  the safe side. What the verdict shows comes in two steps (owner,
  2026-09-25): **the fact** — the pink "5 Mbps · cap" on the viewer's link
  (the compact chain, which has no notes, puts the player's lock at the start
  of the link's line instead: `.tx-lk`, 2026-10-03, so a phone does not say
  it in pink alone; on the line, not before the speed, where its 15 px cut
  "5 Mbps" to "5 Мбит/(" on a 320 px phone), the cap tag in the details (`Viewer.Limited`) — after 3 of thp's events in
  a row (`planFactRun`), off at the verdict's first miss; **the plan box**
  (`Viewer.PlanBox`) after 8 s of it (`PlanBoxAfter`: the first event and 8
  after it) and, once up, until the verdict has been off for 10 s
  (`PlanBoxHold`, the 10th event without it). **The step up is thp's word
  that the cap binds now** (review, 2026-09-26): the verdict turns on, and
  the box rises, only on an event over thp's whole window (`window_sec`;
  `Sample.fullWindow` — from a stream's 6th event) that meets the On pair
  with a request of theirs coming; the rest of a run may be the Off pair's.
  A new stream's first events average over the 1–4 s thp's ring for it holds
  so far: the page's player fetching a 3.5 Mbps file's 4 s segments at a
  5 Mbps cap reads 1.00, 1.00, 0.93 of the cap there and 0.56–0.76 over the
  full window, and judged like full windows that lit the pink link, the cap
  tag and the fact line for a few seconds after every token rotation, reopen
  and web-ui rollout (thp's ring modelled: 3–7 of 40 phases at 3–3.5 Mbps;
  0 of 40 with the rule, `TestMeter_YoungRingNeverTurnsTheVerdictOn`,
  `TestStatusStream_PlanYoungRingIsNotTheCap`). A verdict carried over a
  rotation holds through the young ring. The box rising on a held event
  instead let the window's tail after a download closed — no request since,
  use 0.76, throttled 0.72 — raise it for the second before the viewer left
  (`TestMeter_TheTailRaisesNoBox`, `TestStatusStream_PlanTailRaisesNoBox`).
  Before the box is due the view carries the plan's fact and cap but no
  variant — no box. These are the server's view; what the page draws of the
  box once it has been up is the page's (the box stays, below). For a download and for a player buffering or playing a
  file over the cap no line stands in its place either (`present` draws
  nothing under the bar, so the block grows once, by the box); a player
  playing a file of unknown bitrate gets the fact line and one playing a
  file under the cap with room to spare nothing — neither gets a box while
  it plays. Held through a
  dip the view keeps the tier key and the box while the link shows the speed
  as it is. Counted on thp's events, not the wall clock, like the presence
  debounce. It used to be one step, 15 events, which with thp's five-second
  window ramping up under it (a transfer that reaches the cap mid-stream
  reads 0.2, 0.4… of it for 5 s) made a viewer at the cap wait ~20 s for
  anything; the ramp is still thp's, so "3 s" is from the first event whose
  window is at the cap, and on a stream just opened no sooner than its 6th
  event (`TestStatusStream_PlanFactThenBox`: fact 2.0 s, box 8.0 s, gone
  10.0 s after the last event at the cap;
  `TestStatusStream_PlanBurstShowsTheFactOnly`).
- **The plan box** (`tier`, `cached_tier`, `vaulted_tier`; only once due,
  above) follows the cap modal's targets and only points to a faster plan: a free viewer gets the
  promo plan when it is faster than their cap — its trial via
  `/trial?from=status-bar`, else its checkout, else `/donate`; a paying one
  `/donate` only when a faster plan is on sale (`offer.Service.FasterOnSale`).
  No catalog — no button. The server cannot tell a stream from a download, so
  it sends both variants and the page picks (`present`): its player buffering
  → the stream box, with the player's `data-status-stall-sub` (what the
  stream needs, from the stream job) preferred; playing a file the stream job knows
  needs more than the cap (`statusview.OverCap`, `data-status-over-cap`) → the
  same stream box as soon as it is due, without waiting for a stall
  (`stream_over`; owner, 2026-09-25/26: at the cap the stall is certain, and
  a grace buffer hides it for minutes) — except right after the grace popup:
  once the viewer has answered it ("continue at N Mbps" or its close; the
  player marks the element `data-grace-cta-answered="continue|dismiss"`),
  they have just been told the cap is coming, and the same file gets neither
  the box nor a line (the fact's "no stops" is false for it; the pink link
  still says the cap) until the player's first real stall — then the stream
  box as at any capped stall, and from then on the over-cap rule again
  (owner, 2026-09-26: in Chrome the box came 0.5 s after the popup closed,
  thp binding on the segments past the window; `playerActivity.offerAnswered`).
  The slow-download modal's "watch as is" before playback is the same kind
  of answer: its force-slow run renders the player with
  `data-offer-answered="continue-slow"` (`StreamContent.StatusAnswered`), so a
  viewer who chose to watch slower than the file needs is not sold again
  until the player really stalls (owner, 2026-09-26).
  A stall under way when they answer counts (the video is frozen at the cap
  now); one that ended before the answer does not (the popup spoke for it).
  Since the popup stops the film (2026-09-26) no stall is under way at the
  answer — the popup's pause ends it, before the answer: the first that
  counts is the resumed film's, once its buffer (grown at the cap while the
  popup was up) runs out.
  The mark lives on the element: the next file, a reload or another grace
  window starts without it; no grace popup at all (paid tiers, grace off) —
  no answer, and the box comes once due as above; playing a file of unknown bitrate →
  the fact line, no button; playing a file under the cap with room to spare
  (`statusview.FitsCap`, below) → nothing under the bar; otherwise the
  download box with the file's ETA — only once a download was pressed on
  this page (`a[data-transfer-download]`, until another file is picked): the
  cap is the account's (thp's bucket per session and rate), and another
  device or tab of it, or Stremio on a TV, holds a page that transfers
  nothing at the cap too — there the pink link alone, nothing sold
  (2026-10-03). "Needs" is the
  bitrate of what the player pulls, not the file's (`jobs/scripts`
  `playedBitrate`): one video track and one audio track — the transcoder
  copies H.264 and serves the dub the player starts on (the picker's
  default, not the first: `startAudio`) with its audio as the start's
  declaration has it made (`audioOut`: AAC 5.1 at 384 kbit/s under
  aac51; AAC 5.1 or Dolby copied where declared, Dolby on a passthrough
  only; else stereo AAC, 139.6 kbit/s at 48 kHz), nginx-vod repackages an
  mp4's first tracks as they are —
  while the file's rate counts every dub, commentary and lossless track (24 h
  of transcoder probes, 2026-09-26: of 202 H.264 files over a 5M cap by the
  file's rate, 36 are under it as a stream — a false box while they played
  smoothly). Unknown, and marked neither way: video the transcoder
  re-encodes (the rate is the encoder's choice), no number for the video
  (Matroska: mkvmerge's `BPS` tag) and not every audio track numbered
  either, or statistics tags a later remux left stale (the tracks adding up
  to more than 1.05 of the file). Never over, then; but it can still be
  marked fits by a ceiling (`playedBitrateRouted`): the file's rate for
  tracks served as they are plus an audio encode, and for a re-encode the
  transcoder's `-maxrate` (1.3× its rate for the height,
  `encodedVideoCeiling`) plus the audio — under a 5M cap up to ~550p.
  "Buffering" (`lib/playerActivity.js`) is a real stall in the last 60 s: a
  `waiting` (or a `stalled` short of data) once the element has played since
  its source last (re)started, not while seeking, lasting at least 1.5 s. The
  start of a source is not one — the first play() and a session seek, which
  reloads the source with `seeking` false, both wait for their first data —
  and neither is a hiccup. The stall ends on `playing`, a seek, a pause or
  the end, and on a `timeupdate` only once `currentTime` has moved 0.1 s past
  where the element waited: Chrome's MSE element that runs dry fires one
  more `timeupdate` ~250 ms after `waiting` with the clock where it stopped,
  and taken as playback it closed every stall under 1.5 s — the owner's
  2026-09-25 report ("the video really stalls", the block said "no stops"):
  30 of 30 real stalls of 2.5–215 s at a 5M cap read `playing`
  (`__fixtures__/player-stall-trace.json`, `playerActivity.trace.test.js`).
  A stall whose element has left the page is over, counted as far as it was
  seen: a removed element's `pause`/`emptied` never pass through the
  document — picking another file swaps `#content` before the player's
  teardown runs, "Next" on a prewarmed card pauses and removes in one task —
  and the next file, playing smoothly, read `buffering` for good. A paused player whose buffer still grows is still
  the stream (hls.js buffers far ahead of a paused video, at the cap, for
  longer than that minute), and one the grace popup holds paused
  (`data-grace-cta-hold`) is playing: the pause is the page's, the film goes
  on with the viewer's answer. A file that needs no more than the cap with
  `statusview.FitsMargin` (1.2) to spare (`statusview.FitsCap`; the stream
  job marks the player `data-status-fits-cap`) gets nothing under the bar
  while it plays — and that is all the mark means: a real stall of it while
  the box is due (thp held the viewer at the cap for 8 s) gets the stream
  box like any other, because the stall is then the cap's doing whatever
  the estimate said. Recorded 2026-09-26 at 5M: The Knick s02e01, estimated
  4.56 Mbit/s (4.34 in the cap's megabit) and marked "fits" without the
  margin, pulled 5.29 (5.04 in that megabit) — the first 132 s of video 83.3 MB, 5.05 Mbit/s against
  4.42 for the file less its audio, and the transcoder's AAC 236 kbit/s
  against 139.6 (233–242 over three runs of two files) — and stalled four
  times in 180 s while the page said the cap line alone. 1.15 would have
  kept its mark (0.869 of the cap × 1.15 = 0.999); 1.2 drops it with four
  points to spare, and of 1601 H.264 files probed in 24 h with every number
  known moves 98 from "fits" to unknown (8 more than 1.15). A file within
  the margin is unknown: the fact while it plays, the box at a real stall.
  The box's line says what the file needs only when that is over the cap as
  the labels say (`statusview.OverCap`) — never "up to 5, this file needs
  3", nor "needs 4.6".
  The ETA prices the file the page is on at the cap's own megabit (2^20
  bits, like thp's limiter; `offer.transferSeconds`); picking another file
  swaps only `#content`, so the page reopens the stream with the new `file=`
  (`#file[data-status-file]`). No box comes up on pause, no or few seeders (a few
  seeders slower than the cap win over the viewer being at the cap: they read
  what is cached, the rest waits for the swarm with or without a plan; so do a
  few seeders standing still past the swarm's hold — a speed not known is not
  a fast one — and the rate is compared as it is, not as its label rounds it:
  4.96 reads "5" and is under a cap of 5; `statusview.swarmBound`. A piece
  that comes after more than 2 s the swarm spent fetching moves at what came
  over that time (`statsWatch.gapRate`, `pieceGap`), not at the meter's take
  on its one second: two seeders verifying 4 MiB every 27 s read 13–22 Mbps
  for the whole hold, over the cap, and sold the plan in 578 of 622 frames
  (`TestViewEnv_SlowSwarmIsNotFasterThanItsPieces`). Fetching is a piece
  wanted in the seeder's frames (`statsWatch.wantedSince`): the seeder
  fetches on demand, 20 MiB ahead of the reader, and a fast swarm the
  viewer's reader paces idles between pieces -- over the whole gap it read
  the reader's pace, the cap, as its own, and swapped the box for "a few
  slow seeders" with every piece: 0 frames of 580 sold past the first minute
  at 95% of the cap, up to 118 switches
  (`TestViewEnv_ReaderPacedSwarmIsNotSlow`). No seeder
  and peers — `fewSeeders` counts seeders only — still sells; the owner has
  not decided that one), pieces nobody has (also with the viewer at the cap:
  the file does not finish with a plan either; the pink link stays), a stall,
  a Vault failure, or while another offer is on screen
  (`data-upsell-surface`: the grace popup, the cap modal, the download
  nudge) or on its way — the grace popup from the moment the player's
  element leaves the window by movie time until the player puts it up and
  marks the element `data-grace-cta-shown` (a render effect, a frame or
  more later, none while the tab is hidden; `playerActivity.graceOfferDue`):
  on two clocks a render in between drew the box and the popup folded it
  into a line; and once the viewer has answered the popup, for a file over
  the cap, until their first real stall (above) — the server's held box
  included: its hold keeps it through a dip under the cap, never over a
  stall, few seeders, a Vault failure or a viewer who left
  (`statusview.capped`). That is when a box comes up; one already up on the
  page stays (next). Umami: docs/offers.md.
- **The plan box stays** (owner, 2026-09-27: "the rest of the page keeps
  jumping up and down" — the ~80 px box came and went with the cap, a pause,
  a stall, the viewer leaving, the variant rules, and everything under the
  card moved each time). `present()` still picks what this second calls for;
  `keepBox` folds it into what the page view has drawn (its memory on the
  `#torrent-status` container, so the status token's renewal keeps it; a
  reload or another file starts over — the box goes with the swap of
  `#content`, and no box is taken from the old stream's word, which priced
  the old file, until the new stream says something other than a gap: the
  ticker drawing that word before the new stream spoke put the old box back
  as this page view's, and so did a renewal of the stream in those seconds,
  which held the old word through a gap until the new init no longer kept
  it as the steady one). Once up, the box stays with the last
  words it was sent with — the server's box, while it sends one, updates it
  in place (ETA, download or stream variant) — and the plan's fact and cap
  lines are not drawn over it, nor the pause's and the data gap's hints
  (`BOX_QUIET_HINTS`: they flipped with each burst of the player's buffer
  and moved the page by a line). It goes only with no seeders (`noseed`) or a
  Vault failure (`vault_failed`), where the offer would be false, and at the
  viewer's ×: gone from the card and the sticky bar, none for 24 h in this
  browser on any torrent — also after the status gave up ("unavailable",
  `status.js dead`: the × is the one listener it keeps) —
  (`localStorage["status-plan-box-dismissed"]`, a
  timestamp; this page view remembers it where storage fails), Umami
  `donate-status-bar-dismiss` with the box's props. Its accessible name says
  so (`resource.status.dismissBox`, "Hide this offer for a day, on every
  torrent"; 2026-10-03), not the details popover's plain "Close". The ×, like the rest of
  the page's memory, touches only the box: the pink cap on the chain, the
  grace popup and the player's lock stay. One offer at a time (the grace
  popup or its way up, the cap modal, the download nudge): no box comes up
  while another is on screen — and no line in its place; an up box keeps its
  place with its button and trial note down (`data-quiet`, `visibility:
  hidden`, no href: no click, no impression). Rules and markup:
  docs/transfer_status.html "The box stays".
- **Colour is cause.** A stalled viewer link (open requests, no bytes) is
  amber — the swarm — only while the content comes from the swarm; on cached
  or vaulted content it is drawn neutral with the same "waiting for data".
- **Vault.** `vaulted` no longer ends the resource page's stream (it still
  ends the dashboard's): vaulted content is served through thp too, so
  `vaulted`/`vaulted_tier` show live speed ("Vault ▸ You"), and nobody
  downloading is the "In Vault" badge (`vaulted_idle`). Once the viewer's link cannot come
  (the export named no thp, a final answer, the retries spent:
  `sessionWatch.dead`) the server sends one last message with `final: true`
  and ends the stream; the page closes its EventSource on it rather than let
  it reconnect to the same answer. While the stream stays open on a vaulted
  torrent, the Vault database is asked every 30 s instead of every 2 s.
  Every 2 s is kept for a transfer under way (funded, not vaulted) and for a
  signed-in viewer — a pledge is a click away, and the page does not reopen
  its stream after one, so "Saving" appears on the stream's next read; an
  anonymous viewer of a torrent nobody pledged gets a read every 15 s
  (`vaultPollTicks`; tabs stay open for hours, p99 4.3 h, and the 2 s reads
  were most of the status's load on the database's pool of five). Each read
  is bounded at 2 s (`vaultReadTimeout`), the database row and the Vault API
  together: the loop waits on it, and an unbounded read stopped the stream's
  first message and every tick while the pool was exhausted. A read that
  fails keeps what the last one said, the row and the transfer's progress: a
  database that spends the whole 2 s leaves the API's call an expired
  context, and a transfer under way read "Saving 0%" for as long.
- **Pod shutdown.** `http.Server.Shutdown` waits for every request up to
  `WEB_SHUTDOWN_TIMEOUT` (20 s) without cancelling it, so the status streams
  held every web-ui pod stop for the whole of it and were cut then anyway
  (114 of 158 stops in a week timed out). The server signals the drain
  (`web.Draining`, set from `RegisterOnShutdown` through `BaseContext`):
  the status stream sends `retry:` with a random 1–5 s and ends at once, and
  the page's EventSource reconnects to another pod after that delay, the
  tabs spread out instead of all at once. Other long streams (job logs)
  still drain as before.
- **Token rotation.** thp ends every stream at its token's expiry (10
  minutes). That is planned, not a failure: 30 s before it the watch opens
  the next stream with a fresh token and switches to it the moment it opens
  (the old one is cancelled); the meter only skips the new stream's
  zero-length first event and keeps the speed, the plan's run and its box
  (`Meter.StreamRotated`). The new stream's ring is young: a verdict that is
  on holds through its first events, one that is off cannot turn on until
  the ring covers the whole window (above). A rotation that fails leaves the old stream to its
  expiry, where it is reopened at once — no backoff, no retry budget, a Debug
  line. Before this, every open page lost the plan box for about 15 s every
  ten minutes.
- **Degrading.** Any non-200 is "unavailable". A 4xx (a thp without the route,
  a token thp refuses) and a token that cannot be minted are final; a
  transport error, a 5xx, a 429 or a stream that ended early is reopened at
  most three times (2, 4, 8 s, each spread ×0.5–1.5 so a thp rotation does
  not bring every stream back in the same millisecond; after a 429 thp's
  `Retry-After` when that is longer), each with a fresh token; a reopened
  stream that delivered for a minute gets the budget back. A 429 is thp's
  cap of four streams per torrent and session, taken by the viewer's other
  tabs or by a reconnect overlapping the stream it replaces — a slot frees
  when one of them closes; it used to be final (about 1.7k refusals a day,
  logged at Debug), and is now retried and logged at Info. Either way the
  viewer's link is simply not drawn.
- **Anonymous session id.** It is the hash of the session's id in the store
  (Redis), the same across saves (`api.GenerateSessionID`). It was the hash
  of the session cookie, which the store re-encodes with the time on every
  save — a track pick, a language — so after an autoplay's save the status
  stream reopened under another id than the player's tokens carried, and the
  viewer's link stayed empty for the rest of the episode. The embed without
  cookies hands the same id in raw (`X-Session-ID`, `_sessionID`) and gets
  the same hash; the cookie store (no id) keeps hashing the cookie. The
  switch changed every anonymous viewer's id once (render caches keyed by
  it, thp's limiter buckets and session counters started over).
- **Known gaps.** Whether a paying viewer's traffic through the premium edge
  is counted on the standard-domain node the stream reads is not verified.
- **Rollout order.** thp with `/session-stats` first. A deployed thp without
  the route answers 4xx, which web-ui treats as final: the viewer's link is
  not drawn, the rest of the chain works. Grace tokens with the session
  (2026-09-26): thp with the (session, rate) limiter key and grace-aware
  stats first, then web-ui; never roll thp back past it without rolling
  web-ui back first (docs/grace_token.md "Session").
- **Review:** the debugStatus preview (CLAUDE.md "Transfer status"); the query
  for every design state is in `handlers/resource/debug_states_test.go`.
