# Player (`assets/src/js/lib/player/`)

Preact player for the stream action (`views/action/stream_video.html`, `stream_audio.html`) and
the embed. This file covers the viewer-facing controls; subtitles and AI translation live in
`docs/subtitle_translate.md`, the track picker in `docs/uikit.html` §19.

## Controls and `features`

`parseFeatures` (`Player.jsx`) builds the defaults; an embed may override them through
`settings.features` (the share lock for ad-supported domains is described in the code).

| feature | default | what |
|---|---|---|
| `playpause`, `progress`, `duration`, `volume` | on | the basics |
| `speed` | on | playback speed (below) |
| `advancedtracks`, `fullscreen`, `chromecast`, `embed` | video only | |
| `share`, `logo`, `availableprogress` | see code | |

## Remembered settings — `player-prefs.js`

Volume, mute and playback speed are kept in `localStorage` (`wt-player-prefs`), per browser —
no server, no account. Storage is reached through `safeStorage()` only: in a sandboxed or
third-party iframe reading `window.localStorage` throws, and a player that cannot remember must
still play. Stored values are validated field by field on the way back in.

Restored in `usePlayerState` before the listeners go on. `muted` is restored only towards
silence, so a stored "unmuted" never overrides a stream the page started muted.

## Playback speed — `SpeedControl.jsx`

Scale `RATES` = 0.5 … 2. The button's face is the current rate and lights up when it is not 1×.
Video: a menu above the button — a `popover="manual"` placed by hand from the button's rect, because
`.wt-player` is `overflow: hidden` and on a phone the picture is shorter than the list; the top layer
also keeps it visible in fullscreen (no Popover API → in-flow fallback, clipped but usable). Audio:
the button steps through the scale and wraps — the audio player is one row tall.
Keys `<` / `>` step the rate. `defaultPlaybackRate` is set together with `playbackRate`: a
session seek reloads the source, and `load()` resets the rate to the default.

Not measured yet: whether the transcoder keeps ahead of a 2× viewer on a slow swarm.

## Double-tap to seek — `tap-seek.js`

Touch only (a click counts as a tap when a `touchstart` came within 800 ms before it; with a
mouse a double click stays fullscreen). Two quick taps on the same half of the picture seek
∓10 s through the ordinary `handleSeek`, and every further tap in the streak seeks again; the
feedback bubble shows the running total.

A side tap **waits** for the 300 ms window before it counts as a single tap (play/pause):
otherwise the first tap of a double pauses the film and the second resumes it — a stutter, and a
pair of play/pause events for everything that listens. A
waiting single that turns out not to be half of a double is delivered late, not dropped.

## Keys

space / `k` play-pause · ←/→ ∓15 s · ↑/↓ volume · `f` fullscreen · `m` mute · `<`/`>` speed ·
`g`/`h` subtitles earlier/later. Speed and delay keys answer with a toast (`.wt-player-toast`) —
they have no other face. While the grace popup is up, Play (space/`k`, the big button, a click
on the picture, the headset) is its answer "continue" — below.

## The grace popup holds the film — `grace-hold.js`

The free grace window's popup (`#grace-cta`, docs/grace_token.md) stops the film when it comes up
and the answer resumes it — only if the popup was what stopped it (owner, 2026-09-26). The hold
pauses a playing element and pauses back anything that starts it behind the popup (on the `play`
event: the element's `autoplay`, re-armed by a seek's reload; the subtitle catch-up; the embed's
`player_play`); a session seek asks it before starting its new run (`holdPlayback`), loads the run
without playing it and lets go at `canplay`. "Continue"
and the close resume held playback; a film the viewer had paused, or a seek that landed paused,
stays paused. Play while the popup is up answers "continue" and plays whoever paused (`via: play`
in `grace-soft-cta-click`) — not a dead key. The trial link resumes nothing. Next, a teardown or the
next file drop the hold without resuming. While it holds playback the element carries
`data-grace-cta-hold`, and the resource page's transfer status reads the player as playing, not
as a viewer's pause (`lib/playerActivity.js`).

## Loading spinner — `stall-watch.js`

(Since 2026-09-26 the spinner is the buffering label, below; what shows and hides it is this.)
The spinner follows the **clock and the frames**, not the media events. After a seek past the
buffer `waiting` may never fire, and `seeked` / `canplay` fire when the first fragment is in —
well before the picture moves again (first seconds of a film, buffer shorter than the 10 s of a
double tap). `usePlayerState` samples the element every animation frame:

- **Into a stall:** not paused, not ended, no session seek in flight, `currentTime` unchanged for
  `STALL_MS` (300 ms).
- **Out of it:** evidence of playback, not a changed number — hls.js nudges `currentTime` forward
  while it sits in a buffer hole, and the first version took a nudge for "moving". With a frame
  counter (`getVideoPlaybackQuality`, only when `videoWidth > 0`): `RECOVER_FRAMES` (3) frames
  presented since the stall began — one is not enough, a finished seek paints its target frame
  and stands. Without one (audio, old browsers): the clock advancing on every sample for
  `RECOVER_MS` (250 ms).
- While the watchdog says "stalled", `canplay` / `playing` do not take the spinner down.

## Buffering label — `BufferingLabel.jsx`, `buffering-label.js`

Owner, 2026-09-26 (the canvas "Плеер: буферизация вместо спиннера", variant A). Where the spinner
was, and exactly when it was shown (a stall of the playing film, a start, a session seek, the
translation hold, the next file loading, a moved-to player before its first frame), the player
shows a compact pill: a small spinner and `player.buffering` ("Buffering"; 32 px, 36 px on touch,
40 px in fullscreen with a mouse). The plain pill is a status line (`role="status"`) and takes no
clicks: they go through to the picture, as they went through the spinner.

**At the plan's cap the pill is the lock**: a button "Buffering | [lock] 5 Mbps ›" with the viewer's
own cap (a paying viewer's own, "20 Mbps"), 44 px on touch. **Wherever the pill is shown** — the film
stalling, a seek (a session seek's new run or one inside the run), the next file starting, the hold
after a seek — every one of those waits comes through the limiter that holds the viewer (owner,
2026-09-26: "after a seek at the cap the pill says just Buffering, but the seek wait is limited by the
plan too"). It is plain inside the free grace window by the player's own movie time, while the grace
popup is up (or comes up this very render — a session seek past the window puts it up as the seek
starts, and the seek's wait is on screen behind it), and whenever the status has no word on the cap —
**unless the viewer has answered the grace popup** (below): from the answer on it is the lock at once,
until the status has a word of its own.

**Colours** (owner, 2026-09-26: on a white frame the lock's hover turned the pill into a light-pink
blob, the white "Buffering" gone — 1.02:1). The pill is dark in every state, `rgba(10,14,26,.9)`;
hovered or open only a white layer of 0.08 over the same base and a brighter edge, as the player's
other buttons. No pink fill anywhere on it: the pink is the cap's number, lock and chevron alone. On a
pure white frame the label reads 12.2:1 and the cap 6.1:1, hovered 9.6:1 and 4.8:1 —
`buffering-label.test.js` computes these from `player.css` and fails below 4.5:1. The keyboard's
ring is pink on a dark halo, seen on any frame.

**The card** is the transfer status's stream plan box — `.tx-pbox`, the status's own box (bolt, "the
video loads slower than it plays", the stream job's `data-status-stall-sub` line, the button
`offer.watchUncapped` and the trial note) with a close × — in a native `<dialog class="modal">`
opened with `showModal()`, like `#subtitles`, `#embed` and the grace popup. The top layer: centred on
the screen over DaisyUI's dimmed backdrop, never cut by the player's rounded `overflow:hidden` frame
(it was, inside the player — the owner's stage screenshot, 2026-09-26), and over a fullscreen player
(the top layer is above the fullscreen element). The dialog is the box's size container: side by side
from 670 px of screen, the text over a full-width 44 px button below that. **The card is the page's,
not the player's**: a Preact root of its own on `<body>` (`BufferingLabel.jsx` `openCapCard`), its
state the document's (open is "`.wt-cap-card-host` is on the page", a change is a `player_cap_card`
event on `window`). Rendered inside the player it went with it: moving to the next file destroys the
player it was opened from (`destroyPlayer({ keepStage: true })`) and a card opened while the next file
loaded — or at a stall near the end, before autoplay moved on under it — vanished with nobody closing
it (review, 2026-09-27). A top-layer dialog is on top wherever it sits in the document, over a
fullscreen stage as the grace popup is. The player on screen, whichever one it is by then, reads it
for its lock's open state (on its first frame and on subscribing). It stops its clicks,
double-clicks and keys from reaching the player (the shortcuts are on the document, where its keys
would bubble). Focus starts on the box itself (`tabindex="-1"`, no ring on a button the viewer did
not reach by keyboard); Tab reaches the ×, the button and the backdrop's close.

**Only the viewer closes it**: its ×, Esc (`cancel`), a click beside it (the backdrop form, as
`#subtitles` has), its button (the trial opens in a new tab), or the lock again where the card is
not modal. It stays open, saying what it said when opened, when the film plays again, the status
takes its word back (owner, 2026-09-26) or the next file's player replaces the one it was opened from
— the card is the label as it was, not the live one. Leaving the page (the view's `destroyPlayer()`,
without `keepStage`) takes it. A close the browser makes itself (Chrome's close watcher does not let
every Esc be cancelled) closes it for the player too. Without `showModal` (jsdom; a browser without
`<dialog>`) it is shown by DaisyUI's `.modal-open` class, and closes the same ways; in fullscreen it
leaves fullscreen first — a plain element on the page, outside the fullscreen stage, would be hidden
by it — as the grace popup's fallback does.

**Whether the viewer is held at the cap is the transfer status's word, not the player's.** The status
on the same page (`app/resource/status.js`) asks `lib/transferStatus.js` `playerLabel` on every draw:
the label stands whenever the view says the viewer is at the plan's cap (`view.plan`, the pink fact)
**and** the card's data came with it — the stream box (the server sends the variants only once the
cap has held for the box, `statusview.PlanBoxAfter`, and only with something faster on sale) and the
player's own link (`plan.player`, below). Not the player's own stall verdict: a seek's wait is not a
stall to `playerActivity`, and the lock was missing there. Taken back inside the free grace window
and while another offer is on screen or on its way (the grace popup: one offer at a time).
`present()`'s rules for the block's own box and line (the player's verdict, the file against the
cap, an answered grace popup) stay the block's. It publishes the label on `window` with an event
(`lib/playerLabel.js`: two bundles, two module copies — CLAUDE.md, shared JS state), only when it
changes, from the view the sticky bar keeps through a one-second gap in the data, and takes it back on
teardown. No status on the page (an embed): no label, the plain pill always. A swarm or network stall
has no plan, and a cap before its box is due has no card: the plain pill. With the label goes **the
cause** the view names when a wait is *not* the cap's (`playerCause`, `''` for none): `swarm` (a few
seeders slower than the cap — statusview refuses to sell there, "selling one there quotes a wait it
cannot keep"), `noseed`, `missing` / `missing_idle` / `vault_missing` (pieces nobody connected has), `stalled` (a request of
the viewer's open 5 s without a byte) and `checking`. `stalled` is never the limiter: thp throttles
only the viewer's own responses (`External`, torrent-http-proxy `web.go`), and a bucket lets the cap
through; content-transcoder reads its source through thp's internal URL, uncapped, so a session
seek's playlist held while FFmpeg makes the first segment (`WaitForPlaylist`) is the transcoder's or
the swarm's wait — once the segment is there its bytes come at the cap, and the lock comes back. Not
a cause: the cap itself (box or not), the viewer's bytes flowing with no verdict on what binds them
(`active`, `cached_flow` — right after the grace answer, exactly the gap before thp's verdict), no
viewer on the chain, a gap. A null label alone cannot tell "no verdict yet" from "not the
cap"; the cause says the second out loud, for the answer's lock below.

The player adds only what it knows at this very moment (`capLock`, the label can be a second old):
by its own clock (a session seek's target included) the film is past its free grace window, and its
grace popup is not up or coming up.

**After the grace answer the player draws the lock itself** (owner, 2026-09-27: "after a seek the
20-minute popup appeared; after I answered that I want slow, a plain Buffering hung with no lock").
The viewer's answer — "continue at 5 Mbps", the close, or Play while the popup is up
(`data-grace-cta-answered` on the element) — says the rest of the film is at the cap, but the status's
word on it comes 8–15 s later (thp's verdict after ~3 events, the box 8 s after that), and the popup
kept the lock down while it was up. So once answered, **every wait the pill shows** (a stall, a seek,
the hold after one) is the lock at once, with the card the stream job rendered on the `<video>`
(`buffering-label.js` `answerLabel`, below). The status's label, the moment it is there, is the lock
instead (one source of truth).

**The answer covers only the gap before the status's word, never a word that says no** (review,
2026-09-27: a swarm-bound torrent kept the answer's lock and its trial pitch for the rest of the film,
while the block under the player blamed the swarm). Two words end it:

- the status names another cause for the wait (`playerCause` above: the swarm, no seeders, pieces
  nobody has, nothing flowing) — the plain pill, for as long as it names it; with the cause gone and
  no label yet (the bytes flow again, no verdict) the answer's lock stands in again;
- the status's own label has been up on this stretch of the film past the window (`statusSpoke`):
  from then on the lock is the status's alone, and its label taken back means the plain pill, not the
  answer's lock again. It takes it back only on a word: a stall or another cause at once, 10 s of the
  viewer under the cap (`statusview.PlanBoxHold`: the cap no longer binds), or no word at all any more
  (a gap in the data past 8 s, the status torn down). A seek back
  inside the window starts a new stretch: the status takes its label back there for the window, not
  for the cap, and past the window again the 8–15 s before its verdict are the answer's again.

Not covered: a swarm slower than the cap with more than three seeders (`fewSeeders`) — statusview
names no cause there (`active`: the viewer's bytes flow, not at the cap), so until the status's label
has come on that stretch the answer's lock stands for its waits. The status block says nothing there
either; a cause for it would be statusview's to add, not the player's to guess.

Plain all the same:

- back inside the grace window by movie time (a seek back before 20:00 plays at the grace rate again);
- the stream job says the file fits under the cap with room to spare (`data-status-fits-cap`) — a wait
  there is not the cap's as far as anyone knows before the server says so (the status's own word
  still makes it the lock);
- nothing faster on sale (no catalog, a promo plan no faster than the cap): the job renders no card;
- the next file loading (that wait is the next file's start, inside its own grace window), and the
  next file itself — a new element, without the answer — until the status says so;
- an embed: the job renders no card there (the lock has never been drawn in an embed; a new upsell on
  someone else's site is not this change's to add).

The status block's own rules for its box after the answer (`present()`: no box until the player's
first real stall) do not change — they are the block's, not the label's.

**The card for the answer** is rendered once into the page by the stream job, on the player's
`<video>` (`templates/views/action/stream_video.html`, `jobs/scripts/cap_card.go` `CapCard`, only
where there is a grace window, outside an embed, with a cap in the claims and something faster on
sale): `data-cap-card-rate` (the cap as the lock says it, `statusview.RateLabel` — the status's own
function), `-title` (`resource.status.streamStallTitle`), `-cta` (`offer.watchUncapped`), `-url` (the
promo plan through `trialURL … "player-label"`, else its checkout, else `/donate` — as the status's
`plan.player.url`), `-target`, `-note` (`offer.trialNote`, with a trial only), `-sub` (the cap alone,
`statusview.CapLine`) and the props `-auth`/`-tier`. Its line is the element's `data-status-stall-sub`
when the job knows what the file needs — the very line the status's label takes — and `-sub`
otherwise. Everything goes through `html/template`'s attribute escaping; the player takes the link
only as a path of ours or an https URL. `services/template/cap_card_render_test.go` renders it for
every language and offer state and compares it with the status's own stream box for the same viewer
(`statusview.Build`); it also writes the `__fixtures__/cap-card-video.html` the wiring tests run on.

**One source of truth.** The card has no copy of its own: its title, line, button label and note are
the stream box's, and its link is the server's `plan.player.url` — the stream box's destination
through the player's own `/trial` surface, `offer.FromPlayerLabel` (`/trial?from=player-label`; a
checkout or `/donate` link is the box's own), built in `services/statusview` next to the box's
`status-bar` link, so the two count apart in `webui_trial_shortlink_total{from}`. `plan.player.rate`
is the lock's number (`speed()`, a no-break space before the unit). Events: `player-label-lock-shown`
(the lock drawn, once per player and set of props), `donate-player-label-shown` (the card opened: on
screen because the viewer asked), `donate-player-label` (its button, Umami's own click), with the
status box's props, `location` `player` and `state` `stream_stall` (the stream box at a wait, whatever
the block shows at the time), and `source` — who raised the lock: `status` (the status's label) or
`grace-answer` (the stream job's card after the answer). One lock seen is one lock seen: the answer's
lock the status then takes over is counted once, with the source that raised it first (`lockKey`: the
props but for `source`); the card's opening and its button carry the source of the card opened.

## Loader restart on a stall — `loader-restart.js`

hls.js reports a stall once (the non-fatal `bufferStalledError`, its gap-controller). The player
used to answer every report with `setTimeout(() => hls.startLoad(), 5000)` — and
`StreamController.startLoad()` calls `stopLoad()` first, which **aborts the fragment in flight**
and drops it from the fragment tracker. At the plan's cap a stall is exactly the moment the needed
segment is still arriving, so every stall threw it away 5 s in and fetched it again from byte 0.
Recorded in Chrome 154 at 5M (2026-09-25/26): 48 aborts after one seek; the owner's session had 37
of 59 video segments requested 2–9 times and played at ~0.43× real time where the cap alone
allows ~0.56×; one run skipped the aborted segment for good, left a 4 s hole in the video buffer and
froze for the remaining 215 s.

Nobody recorded why the restart was there: it came with the mediaelement.js player (f65afdea,
2024-03-31, next to a commented-out `recoverMediaError()`, hls.js 1.5.6 from a CDN with default
retries) and was carried into the Preact player (9f80e0ba). A `startLoad()` can only fix a loader
that is not loading what the playhead needs — stopped, or left in `ERROR` by a fatal error — so
that is all it is kept for. The restart now needs **5 s straight** (`RESTART_AFTER_MS`, checked
every second) of:

- the element starving: playing, not seeking, `readyState` below `HAVE_FUTURE_DATA`;
- **nothing in flight** in any stream controller (`hls.inFlightFragments`: main, alternate audio,
  subtitles) — hls.js's own reading (gap-controller `inFlight()`): a fragment in any state but
  `IDLE`/`STOPPED`/`ENDED`/`ERROR`, which counts a scheduled retry;
- the buffered ranges unchanged.

With nothing in flight `stopLoad()` has nothing to abort.

**A request that stopped receiving bytes** is in flight but not arriving: headers in, then no byte
(a dead path, a response stuck upstream). hls.js cuts a request without headers at 10 s (its TTFB),
but after the headers only its 120 s load timeout is left (xhr-loader re-arms it at the headers;
progress never does). Reproduced in Chrome 154: a segment hung at 30% of its body froze the picture
for 106–108 s, where the old 5 s `startLoad()` had it playing in 5. So a fragment in
`FRAG_LOADING` **past its headers** (`frag.stats.loading.first > 0`) counts as arriving only while
`frag.stats.loaded` grows — `frag.stats` is the loader's live `LoadStats`
(`fragment-loader.ts`: `frag.stats = loader.stats`; a retry is a new loader, new stats). With every
in-flight load in that state and **not one byte for 10 s** (`NO_BYTES_MS`) — still starving, the
buffer unchanged — the loader is restarted, which aborts the dead request and asks again. Before
its headers a load stays hls.js's (its TTFB fires first). 10 s is hls.js's own "no byte yet" bound,
and 33× the longest gap measured at the cap: Chrome at 5M against prod thp, an over-cap 1080p file
with a session seek, HTTP/2 — 4085 gaps between body chunks, p99 0.22 s, max 0.30 s; every segment
after the seek requested once, no restart at 9 stall reports. A/B on a local server (Chrome 154,
same hls.js): hung at 30% after headers — old hack 5.0 s frozen, nothing-in-flight rule alone 106 s,
this rule 10.1 s; body in 5 chunks 7 s apart — the old hack aborted it mid-body, this rule let it
finish (1 request); no headers for 30 s — left to hls.js's TTFB (7.8 s frozen, the old hack 5.1 s).

Everything else is hls.js's: a fragment that errors or times out is retried by its load policy
(`fragLoadingMaxRetry` in `HLS_CONFIG`, TTFB 10 s, 120 s a load), playlists likewise, holes and
nudges by the gap-controller; the fatal handler restarts loading after a network error that asking
again can cure (on a backoff — see "Network errors and the stream restart"), and after a media error
hls.js 1.6's `recoverMediaError()` restarts it at the playhead itself (1.5.6's did not).
Disarmed by `STALL_RESOLVED`, a pause, the end, `MANIFEST_LOADING` (a session seek's reload),
detaching and destroying.

Not covered: on HTTP/2 (the stream host negotiates h2) a re-request shares the connection, so a
hang of the browser↔edge connection itself is not cured by asking again — only hangs further
upstream are; how often requests hang in production is not measured.

## Network errors and the stream restart — `network-recovery.js`, `stream-restart.js`

**The bug (2026-09-29).** On *any* fatal `NETWORK_ERROR` the player called `hls.startLoad()` at once,
with no delay and no limit (only `levelParsingError` waited 3 s). The transcoder keeps its sessions in
the pod's memory and drops one after 10 min without a request (`sessionInactivityExpiry`) and on every
rollout; `/session/<id>/…` then answers 404 `session not found` (18 bytes; a passthrough init: `init
not found`), and a day later the token in the URLs is refused (403). hls.js 1.6 does not retry a 4xx
(`error-helper.ts retryForHttpStatus`) and a fragment's 404 ends fatal once no other level is left to
switch to — so each `startLoad()` met the same answer at once: up to 66 requests a second from one
tab, a spinner, no error. 2.27M 404 + 594k 403 a day on `~hls` in thp; 114 of 929 viewer sessions a day
hit a dead session. Triggers: idle over 10 min (a pause, a hidden or sleeping tab) 57% of the volume,
a transcoder rollout 13% (every active view dies; 48 on 29.09 06:10Z), abandoned tabs 30%. iOS
(native HLS) never looped and is untouched.

**Most of a dead session's answers are never fatal (found in Chrome, 2026-09-30).** The transcoder's
variants are `#EXT-X-PLAYLIST-TYPE:EVENT` — live to hls.js 1.6.14. A fragment's 404 there is skipped as a
gap (`base-stream-controller.ts onFragmentOrKeyLoadError` → `treatAsGap`: a media fragment of a live level
with no alternate) and the next fragment asked; a playlist's 403/404 is asked again on hls.js's backoff
(`base-playlist-controller.ts checkRetry` retries a `SendAlternateToPenaltyBox` that carries a
`retryConfig`) until `playlistError` reaches `levelLoadingMaxRetry` — 100 in `HLS_CONFIG`. Iteration 1
acted on fatal errors only, so in a real browser a dead session looped 3,044–5,607 404s (~12 a second)
for 300–411 s, a 403 101 times over 324 s, and at the end of an episode the level playlist was polled on a
backoff under a spinner; it came at once only where a subtitle `.vtt` was loading (that one is fatal).
So **a dead session's answer on any loader — the video's, an audio rendition's, a subtitle's, the master's
— fatal or not, stops loading and restarts the stream at the first one** (`network-recovery.js observe`;
Umami `via: error-nonfatal`, `loader`). Every later answer of the dead session (hls.js already walking to
the next fragment, a stall's `startLoad`) stops loading again, quietly. One exception: the transcoder's
`init not found` while hls.js still asks again. Only a live session says it (`sessionRouter` answers
`session not found` first), and one of its three causes is the 10 s wait for a slow run's init
(`passthrough_web.go passthroughInitWait`), which hls.js's own retry rides out — as a restart trigger it
would restart slow passthrough starts every 10 s. It is the session's end only once hls.js gives it up
(fatal), as before. Real hls.js with the player's own `HLS_CONFIG`, jsdom XHRs and a local server
(`network-recovery.hls.test.js`): the video's and an audio rendition's playlist 404, a 403 — non-fatal,
restarted at the first, the playlist asked once; a non-fatal `init not found` left to hls.js.

**What each network error gets now** (`createNetworkRecovery`, wired in `setupHlsEvents`; the
guards of `passthrough.js` still see every error first):

| failure | answer |
|---|---|
| 404 with the transcoder's `session not found` (`init not found`: fatal only), or with no readable body on a `/session/<id>/` URL; 403 — fatal or not | the session (or its token) is gone: loading stays stopped, the player restarts the stream (below) |
| 404 `segment not found` on a session URL | the run was released under the request, the session lives (`serveSegment`): the backoff below |
| 429 (thp's limiter) | `Retry-After` where CORS exposes it (capped at 60 s), else 5 s — thp sends no `Access-Control-Expose-Headers`, so cross-origin it is the 5 s default, thp's own value; then `startLoad` |
| any other 4xx | no retry: the card (below) |
| 5xx, a timeout, status 0, none | `startLoad` after 1 s, 2, 4, 8, 16, then 30 s each; reset by a fragment of the film that loads (a subtitle segment does not count); after 20 rounds in a row — hls.js's own retries after the first fatal error included (below) — the card |
| `levelParsingError` | 3 s, as before |

Where hls.js 1.6.14 puts what this reads, off its source and checked against a real instance with
real XHRs (`network-recovery.hls.test.js`): the status in `data.response.code` (fragments:
`fragment-loader.ts` `onError`; every playlist: `playlist-loader.ts` `handleNetworkError`); the body
not in `response.data` (always `undefined` on an error) but in `data.networkDetails`, the loader's
XHR — `response` as an `ArrayBuffer` for a fragment, `responseText` for a playlist; headers only via
`getAllResponseHeaders()` (asking `getResponseHeader` for an unexposed one logs an error in Chrome).
The error-controller's `onErrorOut` is registered in the `Hls` constructor, before our listener, and
has already called `stopLoad()` on a fatal error. A failed **master** playlist is loaded again with
`loadSource(hls.url)`, not `startLoad()`: hls.js asks for the master only on `MANIFEST_LOADING`
(`playlist-loader.ts` `startLoad` is empty) — the old handler's answer to a fatal manifest error
loaded nothing at all. A new source (a session seek's `loadSource`) is a new start of the backoff;
our own master reload is not. `DESTROYING` clears the timer.

**The two retry layers.** A fatal 5xx or timeout comes only after hls.js's own retries of that request
(`fragLoadingMaxRetry`/`levelLoadingMaxRetry` 100 in `HLS_CONFIG`, at most 10 s apart; a passthrough
retries a timeout twice) — its patience, untouched: ~16 min for a fragment's 5xx, ~17 min for a
timeout (100 × the 10 s TTFB). The fatal error's `stopLoad()` zeroes the counters it retries by
(`level-controller.ts stopLoad`: `fragmentError`, `loadError`; `error-controller.ts stopLoad`:
`playlistError`), so each `startLoad` of ours used to buy all 100 again: a 503 for good (the
transcoder's `restart limit reached`, variant playlists and segments) reached the card after 21 of
hls.js's cycles, ~5.5 h. So from the first fatal error on, hls.js's retries count as attempts too
(`observe`: an `ERROR` with `fatal: false`, a 5xx/timeout/status 0/429 of the film — not a subtitle's,
and not another 4xx: hls.js never retries one, so `fatal: false` there is a level switch, and if it
ends fatal `handle` reads it — a dead session still restarts). A 503 for good now reaches the card ~3 min after the first fatal
error (1 s of ours, then 1+2+4+8 s and 15 × 10 s of hls.js's), ~19 min from the start; a timeout ~3.5
min after it. A passthrough's timeouts, which went through our backoff every third request, get 20
requests after the first fatal error instead of 60: ~7 min to the card, down from ~18 — not measured
how often a passthrough waits that long and then plays. Status 0 (CORS, connection) is not retried by
hls.js: the card after ~8 min of our backoff (1+2+4+8+16 s, then 15 × 30 s), as before. Not covered:
the master's retries happen inside hls.js's loader and send no event (`xhr-loader.ts retry`), so a
master answering 5xx for good still gets a whole cycle per attempt (the transcoder's 503 is not on the
master; thp's is not measured).

**Rounds, not requests.** An attempt is a round of failures, not one request's. A round opens with the
first failure after our retry has gone (the fatal error stopped all of hls.js's loaders; our `startLoad`
asks for every track again) — or, with no retry of ours out, `ROUND_MS` (2 s) after the last one opened —
and takes in everything failing while our next retry waits and within 2 s of its opening; for a 429, within
its `Retry-After`. thp's limiter refuses the viewer's session, not a request: the video's, the audio's
and the subtitles' playlists and fragments come back 429 together, 5–6 a round, a round every ~7 s (the
5 s default wait plus thp's 2 s hold). Counted per request (iteration 1), four rounds in 30 s spent all
twenty attempts (Chrome, 2026-09-30). hls.js's own retries after the first fatal error are 1, 2, 4, 8,
then 10 s apart — past 2 s, a round each, so the 5xx arithmetic above stands within a retry or two. A
429 storm with no fatal error at all (no subtitle track loading) is hls.js's alone: its playlist retries
and its gap-skips, never counted.

**The restart** (`stream-restart.js`). A dead session cannot be revived from the browser —
`POST /session/<id>/seek` answers 404 too — so it is a new start of the stream job, at the viewer's
place in the film:

1. **the position**: movie time, `video.currentTime + seekOffset` (the run's offset, the same number
   as `data-run-offset`), and whether the film was playing — for a session seek that found the
   session gone (`session-seek.js onSessionGone`, hls.js path only), the seek's target. Such a seek
   moves neither the element nor the offset (it throws before either), so the element still stands
   where the viewer was before it — on a restarted player whose resume seek found its session gone,
   at the run's 0. The target stays the viewer's place for everything that follows: the automatic
   restart, one that waited for the grace popup, and the card's click — whether that seek opened the
   card, came while it waited, or came under a card already up (which swallows the report itself) —
   until the viewer seeks again (`createRecoveryPolicy` `seeking`, from `handleSeek`: a local seek
   moves the element, and its place is theirs) or a fragment of the film loads (`resumed`). Review
   F2, 2026-09-30: past the budget the card dropped the target, and its click restarted at the old
   place — the note and a signed-in viewer's PUT with it (`forceSendPosition` has no 30 s floor, so a
   restarted player's click at 0.x overwrote the saved position). Written as a
   note in `sessionStorage` (`wt-stream-restart`, 10 min); the next player of the same file takes it
   on mounting and seeks there **without the resume prompt** — exactly there, the last tenth of the
   film included (`resumeAt`'s "start from the top" is about coming back to a file). Not
   `/watch/position`: it answers 204 to a viewer without an account and saves nothing under 30 s. A
   signed-in viewer's position is still sent there too (`forceSendPosition`). **Only a place the viewer
   is at is carried** — the note and the PUT both: past the resume question (the saved position
   fetched, and the prompt answered where there is one — video only) and past 0. A session found dead
   at mounting (a replayed job: the job id stays 10 min on the site, an hour in the embed, and a
   rollout in between kills its session) answers the master 404 within a second, before any of that: a
   note at 0 there made the next player skip `/watch/position`, and a signed-in viewer's saved
   position was never offered — then overwritten by the new player's saves from 0. With the question
   open, the film stands at autoplay's fraction of a second before the hold, and a PUT of that would
   overwrite the saved position. A note at 0 is never written: it would only make the next player
   skip the server's copy. ("Continue" answered under the card, its seek finding the session gone, is
   no longer such a case: the click restarts at the answer — iteration 1 restarted from the element's
   0 and left the question to the server's copy, asked again.) Without a note the next player asks as
   a fresh one does;
2. **the restart, with `purge=true`**: a finished job is replayed to anyone who asks for its id, and
   the id changes only every 10 min on the site (every hour in the embed) — a replay hands back the
   very session that is gone (a rollout kills sessions minutes after their start). On the
   resource page, this file's start form (`form[action$=/stream-video|/stream-audio]` with this
   resource and item — the item from `data-item-id` on the element or on its `#subtitles` dialog) is
   submitted again, as the preferred-language change does (Turnstile, the declaration hook); the
   purge field it gets is marked (`data-stream-restart`) and taken off by the next player — until
   then it rides on every start from that form, so a restart that ends before any player mounts (the
   no-peers or the slow-download modal, an error) leaves the viewer's next press of the button purged
   too, once. The deep link's fields are marked the same way (`app/resource/get.js`): before
   2026-09-30 they stayed on the form for good, and every later press on that page purged. When the
   form is another file's (a quiet move to the next episode) or there is none (an audio render has
   no dialog), the deep link
   `?file=<path>#action=stream&purge=true` is loaded (`passthrough.js loadDocument`). In an embed,
   its POST again (`postEmbedStart`) with the declaration it started with and `purge=true` — the
   embed POST reads `purge` since 2026-09-29 (`handlers/embed/post.go`);
   **The viewer's answers ride along.** Where this run was the slow-download modal's "watch as is"
   (a free viewer over the cap; the force-slow start renders `data-offer-answered`,
   `StreamContent.StatusAnswered`), the restart carries `force-slow=true` too — on the start form (marked
   and taken off by the next player, like the purge), on the deep link (`&force-slow=true`, read by
   `app/resource/get.js`). Without it the job ran its gate again and put the modal up before any player
   (Chrome, 2026-09-30: `slow-download-shown` right after `player-recover-restart`, the modal below the
   fold on a phone). And where a restart still ends on that modal (the viewer had not answered it before),
   its own "watch as is" carries `purge=true` (`SlowDownloadData.Purge`, set from the start's purge by
   `ErrorWrapperScript.resubmitContext`): the force-slow job of the same 10-minute bucket is the viewer's
   earlier one, whose session is the dead one — replayed, it was a master 404, the card, and a player
   that never became ready. The grace popup's answer (`data-grace-cta-answered`: `continue` or `dismiss`)
   goes in the note; the next player marks its element answered and shown
   (`data-grace-cta-answered`, `data-grace-cta-shown`) — so the popup is not shown again, and the transfer
   status (`playerActivity.js offerAnswered`) and the buffering label read the answer as on any answered
   player. The embed's POST carries neither: its start never asks;
3. **at most one automatic restart per file every 5 min, per tab** (`wt-stream-restarts` in
   `sessionStorage`). A session that dies again within five minutes is not a pause the viewer took:
   the card instead. Where the budget cannot be read or written (storage blocked — an embed with
   third-party storage off), no automatic restart at all: an unrecorded restart is no limit; the card
   comes at once and its click restarts (from the beginning or the server's saved position there:
   the note cannot be written either);
4. **never while the grace popup is up or on its way** (the film past `data-grace-duration-sec` and
   the popup not shown yet): loading stays stopped and the restart waits for the viewer's answer,
   then goes. Not while the next file loads (`nextLoading`): that player is leaving anyway;
5. **the exact place.** The restarted session's run starts at the transcoder's quantized seek point
   (`floor(t/30)*30`, or the keyframe before it), so the resume's session seek brought the viewer back
   up to 30 s early (Chrome: 21.2 → 1.2, 150 → 118.2, 345 → 324.5, 525 → 510.3). Once the resume's seek
   has settled and the new run has written the place (`local-seek.js exactPlace`: inside what is
   produced, `EDGE_S` from its end), the film goes there by a `currentTime` write — no second session
   seek (`Player.jsx exactRestartPlace`, checked every 500 ms for at most 2 min). Nothing is done within
   a second of it, or past it; any seek of the viewer's cancels it. hls.js path only: native HLS is left
   as it was. A paused restart gets there when the viewer presses Play (below): the checks and their 2
   minutes start with that Play, the first one at once — a viewer back after ten minutes still gets
   there, and hears at most a moment of the quantized point.

**A paused film comes back paused** (Chrome, 2026-09-30: 5 of 5 paused restarts started playing by
themselves 1.7–2.5 s after the restart's POST — a viewer who paused and left the tab got the film's sound
from a background tab after a transcoder rollout). The cause is the element's `autoplay`
(`stream_video.html`, `stream_audio.html`): Chrome plays a paused element with `autoplay` by itself once
a new source has data, and every reload re-arms it — hls.js's `attachMedia` at mount, and its
`loadSource` on the resume's session seek (it detaches and re-attaches the element: the media element
load algorithm sets the "can autoplay" flag again). Checked in headless Chrome 154 on a plain `<video>`:
play, pause, reload → `play`, `playing` at `canplay`; the same with `autoplay` off → paused at
`readyState` 4. (The grace popup's hold had met the same re-arming on 2026-09-26 and pauses it back.)
"The seek settles on `playing`" was that autoplay: nothing else plays a paused seek's new run. So the
restarted player of a paused film (the note's `play: false`) takes `autoplay` off its element before
hls.js attaches and puts it back at the viewer's own Play (`Player.jsx`, the note's block); in an embed,
the page's own start once the player is ready (`player_play`, `app/embed/index.js startPlayer` — autoplay
by another name) is held the same way. A session seek that does not play, on an element without
`autoplay`, settles when the new run can play (`session-seek.js landsPaused`: `canplay`, the freeze
frame gone, seeking unlocked — as a seek held by the grace popup does), not on a `playing` that nothing
would cause: the film lands paused, the big Play up, the timeline at the note's place (the seek's
target; it moves only while playing) over the run's first frame, up to 30 s before it; Play plays it
and the exact place (5) follows. Seeks the viewer makes before that Play land paused too; after it the
element is as every other (a paused session seek's new run plays by itself, as it always has). A
playing film's restart keeps its `autoplay`. The dead-player watch of such a player is armed by the
viewer's Play, not at mount (`dead-player.js` arms at mount on `autoplay` only): a paused film nobody
asked to play is not dead. Native HLS is left as it was (its session seek starts the new run itself;
the recovery never restarts there).

**One owner of what the viewer sees.** While a guard of `passthrough.js` has begun its own restart (the
passthrough guard's old route; the audio guard's file without its multichannel audio — which also stops
and detaches hls.js) or `player-dead` has declared the player dead and it has not revived
(`dead-player.js` `dead`), the recovery does nothing at all: no restart, no card, no event
(`createRecoveryPolicy` `blocked`). A dead player is dead for a reason of its own that a new session
would not cure, and its verdict is the one message. The other way round: once the recovery has taken the
player over (`engaged`: a restart gone out, its card up or waiting for the buffer, a restart waiting for
the grace popup), the dead-player watch stands down (`handled`), so the old player left on the page while
the restarted job runs (a median 58 s) is not reported dead; and a guard's failure after a restart of ours
has gone out restarts nothing on top (its `fallback` is skipped; the next player's guard sees the same
failure if it is real).

**The card** (`.wt-recover`, `Player.jsx`): the film paused, loading stopped, one line
`player.streamLost` and one glass button `player.resumeWatching` ("Продолжить просмотр") — the
resume prompt's vocabulary, see `docs/uikit.html` §22. Shown past the budget (reason `limit`), for
another 4xx and when the backoff ran out (reason `network`). Its click restarts at the viewer's
place (**The restart**, 1: a dead seek's target, else the element's), playing, and counts in the
budget. **It never stops a film that still plays from its buffer**: while the element plays with data
ahead (`readyState` ≥ 3), the card waits — nothing stopped, nothing counted — and comes when the
element starves (`waiting`/`stalled`, `readyState` < 3) or the viewer pauses; not on a pause of the
page's own (a session seek's, the grace popup's hold) nor at the film's end. A fragment of the film
loading meanwhile drops it. Another report while it waits asks the element again: a far seek that
found the session gone paused the old run itself while it was out (no starving then) and never plays
it again, so no event of the element's follows — the waiting card never came, and the viewer had a
paused film with the timeline at the target (review F2). Chrome, 2026-09-30: the 429 give-up paused a
film at `readyState` 4 just as the refusals stopped, and the card stayed. So the give-up (`network-recovery.js giveUp`) no longer stops
loading itself: a fatal error has stopped hls.js already, and after hls.js's own counted retries its
paced ones go on under the waiting card — one that loads takes it back; the card stops loading when it
shows. It takes the big play button's place and hides the buffering pill. **It comes down when
a fragment of the film loads under it** (`network-recovery.js onFilmLoaded` → the policy's
`resumed`): the keyboard and the media session still play and seek under the card, a stall's
`startLoad` (loader-restart) or a session seek's reload can bring the stream back, and the film would
otherwise play under a scrim that blocks the controls, with every later dead session swallowed by the
open card. The network watch resumes with it (after a give-up it waited for a new source only). A
subtitle segment does not count.

**Seen before the first frame too.** The resource page keeps a rendered player out of sight, under the
job's log, until `player_ready` (`canplay`; `app/action.js`). A restarted job whose new session is dead
at its first request never gets there: the card went up inside the hidden player, `card-shown` was
counted, and the viewer read "ожидание инициализации плеера" for minutes (Chrome, 2026-09-30, scenario
9b; `player-dead` stays silent while the recovery is engaged, so there was no message at all). The card
now also sends `player_show` (a window event), on which `action.js` shows the player and hides the log,
as on `player_ready`: the card is the one message, where the log was. Not in the embed: its page
(`app/embed/index.js`) shows the player on `player_ready` only, with a `player_play` and the ads' turn —
not changed here.

**The page's own button after the card.** The viewer who pressed the page's "Смотреть" instead of the
card's button got the job cache's replay — the same 10-minute job id, the job with the dead session in
it — and the card again, hidden (9c). The card marks this file's start form now (`stream-restart.js
markNextStart`, the restart's marked fields: `purge`, and `force-slow` where the run was the slow-download
modal's "watch as is"), starting nothing; the next start from the form — the card's click or the
button — goes past the job cache, and the next player takes the fields off; so does the card coming
down (the stream is back: `hideCard`). Where the page has no form of this file (another file's after a
quiet move, an audio render without an item id) nothing is marked; in an embed there is no page button.
The marks are this file's: a move to the next file from under the card (N, the next-item card's button)
starts that file from a copy of this file's form, and `next-item-go.js nextStartForm` drops the marked
fields from the copy — before, the next episode's start carried `purge=true` past its job cache and
`force-slow=true` past its own slow-download question (review of iteration 3).

**Events** (Umami, `player-telemetry.js track`; defaults as in `docs/analytics.md`):
`player-recover-restart {reason: 404|403, status, via: load|error-nonfatal|seek, loader?}` for every
automatic restart (`via`: `load` a fatal hls.js error, `error-nonfatal` an answer hls.js would have asked
again, `seek` a session seek's POST; `loader`: `video|audio|subtitle|master`, hls.js's only);
`player-recover-card-shown {reason: limit|network, status, cause?, via?}` (`cause`: the 404/403 behind a
`limit`, `via` as above; sent when the card shows, not when it starts waiting for the buffer);
`player-recover-card-click {reason, status}`. Read `player-recover-restart` against `stream-start`; after
a transcoder rollout expect a burst of `via: error-nonfatal` (the first answer of most dead sessions).

Verified in a real browser (iteration 1, Chrome 154 headless, desktop and phone, against prod through
a scratch web-ui): the restart with purge, the card and its button, the budget, the grace wait, a seek's
target, the next episode — which found the non-fatal answers, the lost viewer's answers, the quantized
place and the 429 counting fixed above. Iteration 2 in jsdom (real hls.js with jsdom XHRs for playlists;
a subtitle playlist needs attached media, and fragments a MediaSource, so those are the shapes read off
`fragment-loader.ts` and `base-stream-controller.ts`), then in Chrome again (2026-09-30, 20 scenarios,
17 passed): the three failures were a paused restart playing by itself, the card before the first frame
out of sight (9b), and the page's button replaying the dead job (9c) — fixed above, in jsdom (the
player's wiring with a real `Hls` whose loading is stubbed and an element that autoplays on a reload as
Chrome's does; `app/action.js` with its log fed by hand) and, for the autoplay mechanism alone, a plain
`<video>` in headless Chrome 154 — not the whole run in a browser again. Not verified: how long the
restarted job takes for the viewer (a median 58 s from click to first frame for any start); whether
thp's 429 carries CORS headers (without them the browser sees status 0 — the backoff, not the 5 s); how
often the card waits for the buffer or comes down by itself (no event for either); Firefox and Safari
(the autoplay re-arming is the spec's load algorithm, not checked there).

## Subtitle delay — `cue-offset.js`, `player-prefs.js`

The viewer's correction for a subtitle file that runs early or late: ±0.25 s steps, ±60 s,
positive = later. Buttons in the `#subtitles` dialog header (`#subtitle-delay`, server markup in
`stream_video.html`; painted and driven from `Player.jsx` by delegation on `[data-sub-delay]`,
because the dialog is swapped whole on a preferred-language change), keys `g` / `h`. The reset
button is always in the row and disabled at zero: one that appeared on the first press pushed the
right-aligned row left and landed under the finger that had just pressed `+`.

- The delay lives **on the TextTrack** (`setTrackDelay` → `track.__wtDelay`), and
  `applyCueOffset` reads it there. Cues are re-shifted from five places (a seek, a late
  `<track>` load, a translation reload, …); an argument would have to reach every one of them,
  and the one that was missed would silently drop the correction on the next seek.
- Same arithmetic as the session offset, from the authored times: `abs + delay − offset`, so
  repeated changes never drift. The cue effect in `Player.jsx` therefore runs for direct
  (non-session) streams too, with offset 0.
- **Element-backed tracks only** (OpenSubtitles, uploads, sidecar, AI translation). Subtitles
  muxed into the film are timed by the film and hls.js owns their cues: with such a track
  selected the row is disabled and explains itself.
- Remembered per file (`wt-sub-delay`, `resourceID:path`, capped at 50, zero leaves no entry).
  Per file, not per track: a known simplification — the reset button is next to the value.

## Media Session — `media-session.js`

Lock screen, notification shade, headset and media keys. Title from `data-resource-title`,
artwork from the element's `poster` (already blurred server-side where it must be). Two things
are ours to get right: **position state is reported in film time** (a transcoder run starts at
`seekOffset`; the element's own clock describes the run), and **seeks go through `handleSeek`**
(a session seek is a POST, not a `currentTime` write). `navigator.mediaSession` and
`MediaMetadata` are injected — testable, and a browser without them is a no-op.

## A player that never starts — `dead-player.js`

On 29.09 Safari on a Mac had a dead player on every route for an hour and a half (hls.js on a
ManagedMediaSource swaps in its own `blob:` `<source>`, see `stream-url.js`) and no event said so:
`stream-start` needs 5 s of playback, the passthrough guard watches a decoder. The watch closes
that gap with telemetry only — no restart, the viewer sees nothing.

Armed by `play` (the viewer or autoplay) — and, on a player with `autoplay` in its markup, at
mount: autoplay fires `play` only once there is data (together with `playing`), so a player that
never gets any, or whose element errors before any press (`play()` then rejects without a
`play`), would never be watched. That arming lets go when the element has data (metadata on: an iPhone refuses autoplay with
sound and its ManagedMediaSource streams no further than that) and stays paused (autoplay refused,
the resume prompt's hold); the viewer's Play arms it again. Off at the first
`playing` on a playing element (the hold leaves a `playing` on a paused one) or the clock moving
past where the request found it; a pause disarms (not one the element takes with an error: Chrome
sets the error and pauses in the same moment on an append failure before metadata), a hidden tab
restarts the quiet, a restart a
guard has begun (`guard.done`), a player the stream restart has taken over (`stream-restart.js`
`engaged`: its restart gone out, its card up or waiting — the old player stays on the page while the
restarted job runs, and is not dead) or an element taken off the page ends it. Its verdict is read the
other way too: while `dead` (reported and not revived) the stream restart does nothing (see "Network
errors and the stream restart", one owner).
One `player-dead` per player, when any of three holds 30 s after the request:

- **`why: quiet`** — nothing moved towards playback for 30 s. Progress is an answer or bytes, and
  a request still pending — never the asking itself: on a lost transcoder session (a rollout) every
  request gets a 404 and hls.js asks again for ever. So: no playlist request pending
  (manifest always; video/audio playlist only before the first fragment is in — after that
  hls.js polls a live playlist whatever the element does), no fragment in flight before its
  headers or receiving bytes (`loader-restart.js` `loadProgress`/`loadWork` — a 4K segment at the
  cap raises no event for minutes), no change of the element's buffered ranges or `readyState`,
  no `progress`, and — without hls.js — no `networkState` LOADING without an error. An hls.js
  error is not progress (a retry is a new request, which counts); neither are `emptied`,
  `loadstart`, `durationchange` (a recovery fires them and does nothing else). An error ends
  only the pending request its `details` names.
- **`why: error`** — the element held a MediaError for 30 s, however busy hls.js was. A
  SourceBuffer append failure on the production master shape is a non-fatal `bufferAppendError`
  with the MediaSource still open: hls.js does not recover it, keeps loading, and the element sits
  on MediaError 4 (AAC 5.1 session's bench, headless Chrome 154 + hls.js 1.6.14). An `emptied`
  (any recovery: `load()`) restarts the clock.
- **`why: recovering`** — 5 re-attachments (`emptied`) since the request and no start: on a muxed
  TS shape hls.js recovers ~1000 times a second.

`player-dead` carries the state it died in (`by: play|autoplay`, `paused`, `path`, `hls`, `err`, `rs`, `ns`, `inflight`, `got`,
`recoveries`) and the start's audio class (`audio: none|aac51|dolby`, `passthrough.js`
`startAudioClass`, as on `stream-start`: a multichannel start that dies without an error or a
fallback has no other event to be counted per class by — see "Watching the audio after the
rollout"); `player-revived` follows if it plays after all, with the same `route` and `audio` —
their count is the rule's error.
No timer outlives the report. Not verified in a browser: native HLS in Safari keeping
`networkState` at LOADING while stuck would hide a quiet death there. **Found 2026-09-30, not fixed
here:** in Chrome 154 an append failure before metadata after a `play()` is followed by a `pause` of
the element, which disarms the watch — the `why: error` death on the production master shape is not
reported (bench under "Multichannel audio and the fallback", "Known gap").

## A fragment loaded again and again — `fragment-loop.js`

hls.js loads a fragment, appends it, finds it is not in the buffer and loads it again — two
neighbours, N and N+1, for as long as the page is open: ~2.7 requests a second each, answered by
the browser's cache (the transcoder sees 304s; 6–8 sessions a day, up to 33k requests one, thp logs
2026-09-30). Nothing errors, so neither the passthrough guard nor the network recovery sees it, and
`player-dead` does not either — bytes keep arriving. The viewer sits on a frozen picture.

**One cause is known** (2026-09-30, Chrome 154, a Playwright MSE bench): Chrome silently drops a whole
passthrough fMP4 fragment in which a leading picture (RASL_N) shares the keyframe's (CRA's)
timestamp — the append succeeds, `buffered` does not change. The same fragment with the duplicate's
composition offset moved by one tick is taken whole. The duplicate is necessary, not sufficient: a
fragment of another file with the same pattern is taken, and what else Chrome's rule needs is not
known. The transcoder makes those timestamps unique. Other causes are not known: a loop on iOS
(WebKit, `?mms` hls.js) was on the audio's first fragments, whose fMP4 is clean — most likely
hls.js's audio controller reloading at a stall of the video.

**The watch**, on every hls.js instance (`hls-manager.js` `setupHlsEvents`, the player's `onLoop`):
the same fragment — `type`, `level`, `sn` — loaded `LOOP_LOADS` (6) times within `LOOP_WINDOW_MS`
(60 s) is a loop, whatever the cause; the player is told once. A new manifest (`MANIFEST_LOADING`:
a session seek, a restart) starts the count again — a new run numbers its fragments from 0. A
viewer's seeks back, or a recovery's reload, are one or two loads of a fragment, well under it.
hls.js's own `FRAG_LOADED` listeners run first; one of them throwing ends the event before the watch
(hls.js reports an `INTERNAL_EXCEPTION`) — the wiring test hands a fragment as hls.js makes one.

**The player's answer:**

- Umami `fragment-loop {type, sn, level, loads, fallback, route, cls, audio}` — `type` `main` or
  `audio` (which loader looped), `cls` the video class, `audio` the start's audio class;
- on passthrough, where the old route plays the file (`loopGivesUp`: not the `-2160` classes), the
  guard's fallback with reason `fragment_loop` — the same visible restart from the start as
  "Compatibility mode" (see "Passthrough: errors and fallback"); it strikes no class (the file's
  fault, not the decoder's) and charges no audio class. Over 1080 the old route refuses the file —
  the viewer would read that the browser cannot show 4K HEVC, which is not so, and the memory would
  keep the file refused for 7 days: a frozen picture the viewer can seek past is the lesser harm,
  and the event is all;
- anywhere else, the event is all.

Not verified in a real browser: the watch against a live loop (the bench reproduced the drop, not
hls.js's reloading of it); what WebKit does with the duplicate.

## Usage events — `player-telemetry.js`

One Umami event per **decision**, not per press (`settled()` holds the last value until the
presses stop and is flushed on teardown):

| event | data | when |
|---|---|---|
| `player-speed` | `rate`, `source: menu\|key` | the rate actually changed |
| `player-tap-seek` | `dir: forward\|back`, `seconds` | a streak of double taps ended (900 ms) |
| `subtitle-delay` | `delay`, `source: dialog\|key` | the delay settled (2 s) |
| `player-media-session` | `action: play\|pause\|seek` | first use of each action per player |
| `player-label-lock-shown` | the status box's props, `location: player`, `source: status\|grace-answer` | the buffering label's lock drawn, once per player (see "Buffering label") |
| `donate-player-label-shown` | the same | the lock opened the plan card |
| `stream-start` | + `rate`, `subtitleDelay`, `route`, `reason`, `decl`, `audio` | what the stream started with (remembered settings make no change event); the transcoder session's route and reason and the declaration it was started with (`data-video-route`, `data-route-reason`, `data-decode`; `''` where the element has none) — the base the HEVC passthrough is measured against; `audio` the start's audio class, `none\|aac51\|dolby` (`startAudioClass`: `data-audio-class` where `data-decode` has an audio token, else `none`; since 2026-09-29) — the base the audio is measured against |
| `hevc-fallback` | `reason`, `cls`, `path: mse\|native`, `audio` | a passthrough given up to the old route (see "Passthrough: errors and fallback"); `audio` the start's audio class, as in `stream-start` (since 2026-09-29): a failure of a Dolby session that nothing pinned on the audio is counted here |
| `audio-fallback` | `reason`, `cls: dolby\|aac51`, `path: mse\|native`, `route`, `audio`, `by` | a file restarted without the multichannel audio its declaration made, on any route (see "Multichannel audio and the fallback"); never counted in `hevc-fallback`. `cls` is the class charged, `audio` the start's (they differ where the master names Dolby and the rendition in play is AAC 5.1); `by` what blamed the audio: `buffer` (its SourceBuffer's own append failed), `codec` (its codec refused), `message` (the element's MediaError message), `native` (the old route's native-HLS rule, nothing named) — since 2026-09-29 |
| `player-dead` | `why: quiet\|error\|recovering`, `path: hlsjs\|native\|direct\|blob\|none`, `hls: on\|off\|none`, `route`, `audio`, `err`, `rs`, `ns`, `inflight`, `got: none\|playlist\|frags`, `recoveries`, `waited_s` | asked to play and not started 30 s on: nothing moving, an element error held, or a recovery loop (see "A player that never starts"); once per player |
| `fragment-loop` | `type: main\|audio`, `sn`, `level`, `loads`, `fallback`, `route`, `cls`, `audio` | the same fragment loaded 6 times within 60 s (see "A fragment loaded again and again"); once per hls.js instance; `fallback` whether it gave a passthrough up to the old route — since 2026-09-30 |
| `player-revived` | `path`, `route`, `audio`, `waited_s` (since the play request), `recoveries` | it played after a `player-dead`: the rule's error, count it against `player-dead`. `audio` on both is the start's audio class, as in `stream-start` (since 2026-09-30) |
| `player-recover-restart` | `reason: 404\|403`, `status`, `via: load\|error-nonfatal\|seek`, `loader: video\|audio\|subtitle\|master` (hls.js only) | the stream restarted by itself after its transcoder session (or token) was gone; `error-nonfatal`: an answer hls.js would have asked again (see "Network errors and the stream restart") |
| `player-recover-card-shown` | `reason: limit\|network`, `status`, `cause` (404/403, for `limit`), `via` (for `limit`) | the card instead: the automatic restart spent within 5 min, another 4xx, or the backoff run out; sent when it shows (it waits while the film plays from its buffer) |
| `player-recover-card-click` | `reason`, `status` | its "Continue watching" |

Read them as shares of `stream-start` sessions; mobile share for `player-tap-seek`. The browser is the
Umami session's (`session.browser`, `session.os`: `edge-chromium`, `safari`, `ios`, `crios`, …); no
event carries its own. The queries for the audio are under "Watching the audio after the rollout".

## Codec support — `codec-support.js`

One Umami event, `codec-support`, that answers a transcoder question: what share of the people
who watch could play HEVC or AV1 as it is, if content-transcoder passed it through (fMP4 HLS,
`-c:v copy`) instead of re-encoding it to H.264. About a quarter of fresh sources are HEVC 1080p+
or AV1; their re-encode is slower than realtime and most of the transcoder's CPU.

Since stage 0 of the HEVC passthrough plan the event also carries the **declaration** — the `decode`
tokens this browser will send the transcoder (below), computed by the very function the declaration
will use — and a second event, `playback-quality`, measures what software decoding costs.

**When.** After the first `playing` of a `<video>` the player renders (`whenPlaying`; an element
already playing when the player mounts counts at once), so the count is of viewers, not visitors.
Never for the audio player. The probe and the send wait for an idle callback (10 s deadline; 2 s
timer where there is none). At most once per browser per 7 days (`localStorage`
`wt-codec-support`, the time of the last send, written when it is sent); once per page (a flag on
`window`) where storage is unavailable, so a next-episode move does not report twice. Without
`window.umami` nothing is sent and nothing is remembered. Embeds report too: the player is the
same, and the embed page runs Umami — without `eventDefaults` (no `tier`/`lang`), and with the
week counted per embedding site, since third-party storage is partitioned.

**Payload** (booleans unless stated; every browser API is wrapped — an old browser or a throwing
one answers `false`):

| key | what |
|---|---|
| `mse` | `mse` (MediaSource), `mms` (ManagedMediaSource only — iPhone, iOS 17.1+), `none` |
| `hvc` / `hev` | MSE `isTypeSupported` HEVC Main 1080p, `hvc1.1.6.L120.90` / `hev1…` |
| `hvc10` | HEVC Main10 1080p, `hvc1.2.4.L120.90` |
| `hvc4k` | HEVC Main 4K, `hvc1.1.6.L153.90` |
| `av1` / `av1_10` / `av1_4k` | AV1 8-bit 1080p `av01.0.08M.08` / 10-bit `av01.0.08M.10` / 4K `av01.0.12M.08` |
| `n_hls` | the element plays HLS itself (`canPlayType('application/vnd.apple.mpegurl')`) |
| `n_hvc` | the element plays HEVC in MP4 itself — what native HLS (iOS) would need |
| `n_av1` | the same for AV1 (`av01.0.08M.08`) |
| `mc` | `navigator.mediaCapabilities.decodingInfo` exists |
| `mc_hvc`, `mc_hvc_sm`, `mc_hvc_pe` | its answer for HEVC Main 1080p over MSE: supported, smooth, powerEfficient |
| `mc_av1`, `mc_av1_sm`, `mc_av1_pe` | the same for AV1 8-bit 1080p (3 s timeout → `false`) |
| `hevc8`, `hevc10`, `hevc8-2160`, `hevc10-2160`, `hevc-high`, `hdr-pq` | this browser declares the token (see "The declaration") |
| `aac51`, `ac3`, `ec3` | the same for the audio tokens (multichannel audio, since 2026-09-28) |
| `decode` | string: the tokens this browser answers, joined with `,` — the `decode=` value of a start that takes part in both parts of the declaration and has no failure in memory; `''` for none. **Since the multichannel-audio release (2026-09) it carries the audio tokens too, for every browser** (the event asks every viewer the audio questions, whatever its audio switch, `?audio=`): a browser that reported `hevc8,hevc10` reports `hevc8,hevc10,aac51` — see "Reading `decode`" below. What a start actually sent is `stream-start`'s `decl` |
| `decode_path` | string: the path they were asked on — `mse` (hls.js), `native` (the element's HLS), `none` |
| `dynamic-range` | string: `high` / `standard` (`matchMedia('(dynamic-range: …)')`), `unknown` where the feature is missing |
| `src` | the source's video codec: `h264` / `hevc` / `av1` / `other` / `unknown` (no probe on the page) |
| `tc` | a transcoder session serves this stream (`data-session-id`) |
| `route` | the session's route (`data-video-route`: `passthrough`, `copy`, `reencode`, `audio`; `''` without one) |
| `pl` | how the player plays it: `hlsjs`, `native` (the element's own HLS — iOS always), `direct` |
| `emb` | inside the embed |

For example: `{mse: 'mse', hvc: true, hev: true, hvc10: true, hvc4k: true, av1: true,
av1_10: true, av1_4k: true, n_hls: false, n_hvc: true, n_av1: true, mc: true, mc_hvc: true, mc_hvc_sm: true,
mc_hvc_pe: true, mc_av1: true, mc_av1_sm: true, mc_av1_pe: false, hevc8: true, hevc10: true,
'hevc8-2160': true, 'hevc10-2160': true, 'hevc-high': false, 'hdr-pq': true, aac51: true, ac3: false,
ec3: false, decode: 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq,aac51', decode_path: 'mse',
'dynamic-range': 'standard', src: 'hevc', tc: true, route: 'reencode', pl: 'hlsjs', emb: false}` plus
the usual `tier`, `is_authed`, `user_id`, `lang`, `is_referral` on the site (`docs/analytics.md`) —
40 properties.

**Reading `decode`.** The string is the whole declaration, video tokens then audio tokens, so an
exact match on it (`decode = 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq'`) stops matching from
the release that added the audio tokens for every browser that answers any of them (how many do is
not measured yet — the booleans below will say), and a series built that way drops on that day
without anything having changed in the browsers. Measure a token's share by its own boolean key (`hevc10-2160`, `hdr-pq`, `aac51`, `ec3`, …),
which means the same before and after; read `decode` only as a whole-declaration label, and split
it at `,` when a query needs its video part. The same holds for `playback-quality`'s `decode`, and
for `stream-start`'s `decl` (the declaration the start sent) — which since the audio's stage 5 for
AAC 5.1 (2026-09-29) ends in `,aac51` for nearly every browser: 635 of the 638 `codec-support` events
between the multichannel-audio release (2026-09-28 21:10Z) and 2026-09-29 16:30Z answered `aac51`
(the other 3 carried no audio key: pages from before the release), so a series on the exact `decl`
string drops on the day this ships.

`src` comes from `data-video-codecs` on the `<video>` (`stream_video.html`): every video stream of
the job's media probe, cover pictures included (`mjpeg`/`png`… are skipped client-side). ffprobe
reports the profile and `pix_fmt`, but `api.MediaProbe` does not decode them, so a 10-bit HEVC
source cannot be told from an 8-bit one here: read `hvc10` as what a 10-bit passthrough would need,
and weight `hvc`/`hvc10` by the Main10 share of HEVC sources measured on the server side (the
capability keys do not depend on the source).

**Reading it.** `isTypeSupported` can say yes to a codec the machine decodes in software at a
fraction of realtime; `mc_*_pe` (a hardware decoder) is the conservative answer. The share that
matters is among re-encoded viewers — `tc` and `src` in (`hevc`, `av1`) — split by `pl`: through
hls.js the MSE keys decide, through native HLS `n_hvc` / `n_av1` (iOS always plays HLS natively,
even where it has a ManagedMediaSource). `src`/`tc`/`pl` describe the stream of the
browser's first play *that week*, so the split by source is a sample of browsers, not of plays; the
capability keys do not depend on it.

### The declaration — `decodeTokens`

What the page sends content-transcoder as `decode=<tokens>` on POST `/session` (sent since stage 3,
by browsers that take part — see [Sending it](#sending-it--decode-declarationjs) below). The transcoder alone picks the route; the browser only says what it
decodes. `decodeTokens(env)` → the tokens in this order (unknown ones are ignored by the transcoder,
so renaming one is a protocol change):

| token | codec string asked | means |
|---|---|---|
| `hevc8` | `hvc1.1.6.L123.90` | HEVC Main 8-bit up to 1920×1080, level ≤ 4.1 |
| `hevc10` | `hvc1.2.4.L123.90` | Main10 up to 1920×1080, level ≤ 4.1 |
| `hevc8-2160` | `hvc1.1.6.L153.90` | Main up to 3840×2160, level ≤ 5.1 |
| `hevc10-2160` | `hvc1.2.4.L153.90` | Main10 up to 3840×2160, level ≤ 5.1 |
| `hevc-high` | `hvc1.2.4.H153.90` | tier High (UHD Blu-ray remuxes) |
| `hdr-pq` | `decodingInfo` Main10 3840×2160, `transferFunction: 'pq'`, `colorGamut: 'rec2020'` | decodes PQ (HDR10) |
| `aac51` | MSE: `decodingInfo` `media-source`, `audio/mp4;codecs=mp4a.40.2`, `channels: '6'`, 384 kbit/s, 48 kHz; native: `canPlayType('audio/mp4; codecs="mp4a.40.2"')` | decodes AAC-LC with up to six channels (5.1) |
| `ac3` | `ac-3` | decodes AC-3 (Dolby Digital) |
| `ec3` | `ec-3` | decodes E-AC-3 (Dolby Digital Plus; Atmos is E-AC-3 with JOC) |

Rules (owner's decisions of 2026-09-27 over the plan's §2.2):

- **Any support counts.** An HEVC token is declared when the path the player would take says yes to
  its string: `MediaSource.isTypeSupported` on the MSE path, `video.canPlayType` (`probably` or
  `maybe`) on the native one. No `decodingInfo`, no `powerEfficient`: a software decoder counts, and
  its cost is what `playback-quality` measures.
- **The path is the player's** (`decodePath`, pinned by tests to `hls-manager.js` and the installed
  hls.js): native on iOS/iPadOS (an iPad in desktop mode included) or where `Hls.isSupported()` is
  false and the element plays HLS; hls.js otherwise; `none` without either — nothing is declared. On
  the MSE path the `MediaSource` asked is the one hls.js uses (`ManagedMediaSource` first), in its
  spelling `video/mp4;codecs=…`.
- **Firefox on Windows declares nothing** — the UA rule hls.js applies to that browser's HEVC answers
  (`userAgentHevcSupportIsInaccurate`, `/\(Windows.+Firefox\//i`): the player itself does not believe
  them. It covers `hdr-pq` too (the PQ question names an HEVC codec).
- **`hdr-pq`** is the one asynchronous token: `decodingInfo` as `media-source`, on the MSE path only,
  without `hdrMetadataType`; `supported` is enough. Never on the native path (iOS/iPadOS): native HLS
  refuses a PQ variant without a word — the iPhone fetches the master and nothing after it, no error,
  no fallback (2026-09-29: 0 of 9 PQ passthrough sessions on iPhones past the master in 24 h; the
  owner's iPhone on five real files, PQ 0 of 3, SDR 2 of 2 with VIDEO-RANGE the only difference),
  while `decodingInfo` as `file` said yes. PQ sources are re-encoded there as before passthrough: 1080p
  plays in SDR, 4K is refused with its reason (`needs_pq`, over 1080p). Missing API, a rejection or no
  answer in 3 s → not declared. Asked at 4K, where ~89% of PQ sessions are: a browser that decodes
  1080p PQ but not 4K PQ is under-declared, and its 1080p PQ sources stay re-encoded, as today. The
  screen is not consulted (variant A); `dynamic-range` is only reported.
- **The audio tokens** (multichannel audio, owner's go of 2026-09-28; content-transcoder: AAC 5.1 and
  Dolby as it is). About 40% of sources are multichannel (E-AC-3 24%, AC-3 7%, AAC 7% of 1126 in a
  day) and every one is downmixed to stereo today. With `aac51` a 5.1 AAC track is copied and every
  other multichannel track is encoded to AAC 5.1 instead of stereo, on either route; with `ac3` /
  `ec3` that Dolby track is copied as it is — Atmos included — but only in fMP4, i.e. a passthrough
  session (the full hls.js build refuses E-AC-3 in MPEG-TS: `Unsupported EC-3 in M2TS`). For audio a
  missing token is exactly the audio the transcoder has always made; nothing is refused for want of
  one.
  - **Independent of HEVC.** Asked and declared whatever the browser says about HEVC — Firefox on
    Windows included: hls.js distrusts that browser's HEVC answers and no audio answer of any
    browser (`codecs.ts`, `mediacapabilities-helper.ts`; a test counts the rule's call sites in the
    installed hls.js).
  - **`ac3`, `ec3`**: on the MSE path the question hls.js asks itself before it keeps a level
    (`level-controller.ts` `isAudioSupported`: its MediaSource — `ManagedMediaSource` first — and its
    spelling `audio/mp4;codecs=ec-3`); on the native path `canPlayType`. Synchronous.
  - **`aac51`**: `isTypeSupported` has no word for channels (every MSE browser with AAC says yes to
    `mp4a.40.2`), so on the MSE path `decodingInfo` answers — the same audio configuration hls.js
    itself sends `decodingInfo` for a level whose audio rendition has more than two `CHANNELS`
    (it drops the level on "no" only where the master has more than one). `supported` is enough;
    without `decodingInfo` the token is not declared. On the native path `canPlayType` for AAC,
    which says nothing about channels (the spec's choice). The events give it 3 s, like `hdr-pq`.
- `hevcDecodeTokens(env)` is the synchronous part (the five HEVC tokens), `dolbyDecodeTokens(env)`
  the Dolby one. `declarationSupport(env)` is what the page declares: the HEVC tokens at once; `pq`,
  a promise of the `hdr-pq` answer; `audio`, a promise of the audio tokens that settles once `aac51`
  has its answer. Both promises have **no deadline** (`supportedAnswer`: a rejection, a throw,
  garbage or a missing API is an answer, "no"; only silence is not), and neither waits for the
  other. `decodeTokens` — the events — keeps the 3 s deadline: a count has to close. The declaration
  must not: a check that did not answer is not a browser that cannot decode. (One difference remains
  from stage 3: the events ask `hdr-pq` even of a browser without an HEVC token, the declaration
  does not.)

#### Sending it — `decode-declaration.js`

- **Who.** Every browser, since stage 5 (2026-09-28, `takesPart`: `!== 'off'`), except one that
  opened any page with `?passthrough=off` (localStorage `wt-passthrough`); `?passthrough=on` takes it
  back. Before stage 5 it was the other way round (`=== 'on'`): only browsers that opted in declared.
  Why a per-browser switch and not a flag: the transcoder is one for production and stage, and
  web-ui's stage is `main` itself, so "production does not declare yet" could not be a deployment.
  A page that does not take part sends no field and
  runs no probe; the probe module is loaded (`import()`, chunk `decode-probe`) only when it does.
  The layout carries the rest: +1.3 KB gzip on `layout.js`, +1.4 KB on `embed/check.js` (measured
  with `npm run build`, 2026-09-28); the audio part added 180 B gzip to each, and 337 B to the
  `decode-probe` chunk; the audio classes of the memory of failures another 201 B on `layout.js`,
  218 B on `embed/check.js` (and 161 B on `discover.js`, 1.56 KB on the player chunk with the
  audio guards); the audio part's own opt-in 128 B on `layout.js`, 123 B on `embed/check.js`. All of
  it against stage 5 (`ef1e89a2`): `layout.js` +511 B, `embed/check.js` +523 B, `decode-probe`
  +352 B, `discover.js` +392 B, the player chunk +1.69 KB, `resource/get.js` +103 B. The audio's
  stage 5 for AAC 5.1 with the audio guard's new rule and the cached audio answer in `whenDeclared`
  (against `0e57be3e`, 2026-09-29): `layout.js` +45 B, `embed/check.js` +50 B, `resource/get.js`
  +154 B (`whenDeclared` now reads the audio cache), `discover.js` +61 B, the player chunk +175 B,
  `decode-probe` +2 B. Dolby by default, no "Dolby first", the audio output rule and the audio class
  on the events (against `ce8bf7fc`, 2026-09-29): `layout.js` −7 B, `embed/check.js` −7 B,
  `discover.js` −12 B, the player chunk +126 B; the whole audio stage 5 against `84dea835`:
  `layout.js` +41 B, `embed/check.js` +44 B, `resource/get.js` +155 B, `discover.js` +49 B, the
  player chunk +333 B (`npm run build`, sizes after `gzip -9`). After the merge of `main`
  (`6c8a74e0`), `player-dead`'s `audio` included, against `6c8a74e0`: `layout.js` +40 B,
  `embed/check.js` +41 B, `resource/get.js` +166 B, `discover.js` +51 B, the player chunk +311 B,
  `decode-probe` −1 B.
- **How an iPhone plays HLS: hls.js by default, `?mms=off` opts out.** iOS and iPadOS played HLS
  natively (`hls-manager.js`, 00369751: "ManagedMediaSource is unreliable", reasons not recorded),
  and native HLS refuses a PQ variant without a word (see `hdr-pq` above). Since 2026-09-30 (owner's
  go, after his iPhone played a 4K PQ passthrough this way) every iPhone and iPad with a
  ManagedMediaSource (iOS 17.1+; without one it stays native) plays through hls.js, and the
  declaration asks what hls.js will use: the ManagedMediaSource's `isTypeSupported`, and `hdr-pq` as
  `media-source` (`iosPlaysHlsJs`: `!== 'off'`; `decodePath` via `env.iosHlsJs`). `?mms=off` on any
  page (localStorage `wt-mms`, read once per page) goes back to native, `?mms=on` back to hls.js
  (`applyMmsUrlSwitch`). The `wt-decode` cache entries carry `mms`; another path's is not sent (an
  entry from before `mms` says nothing and is taken). What it costs: hls.js sets
  `disableRemotePlayback` on such an element — no AirPlay. Under hls.js, Play before any data no
  longer calls `load()` (`usePlayerState` togglePlay): it dropped the element's MediaSource.
- **Who declares audio.** Every browser, every audio token it answers, unless it opened
  `?audio=off` — the audio's **stage 5 is done for all three tokens** (AAC 5.1 2026-09-29, Dolby
  2026-09-30; between the two Dolby went out only with `?audio=on`). The audio tokens
  have a switch of their own, independent of the video's: `?audio=off` on any page (localStorage
  `wt-audio`; where storage throws, the page's own state; `applyAudioUrlSwitch`), and `?audio=on`,
  still read and remembered, which now only takes an `off` back. Two predicates, one per question,
  both reading the one switch (`audioSwitch`):
  - **`aac51`** (`mayDeclareAac51`: `!== 'off'`) — since the audio's stage 5 for AAC 5.1
    (2026-09-29, after the owner's device test of 2026-09-29 15:36–15:45Z: Chrome 154 on a Mac
    declaring `aac51`, 8 files — E-AC-3, AC-3 and DTS encoded to AAC 5.1, AAC 5.1 copied, an H.264
    old-route E-AC-3 — 19 FFmpeg starts with 10 seeks, 0 stalls, 0 fallbacks). In practice every
    browser: 635 of 638 `codec-support` events since the multichannel-audio release answered `aac51`
    (MSE: `decodingInfo` says `supported` for six channels; native: `canPlayType` for AAC, which says
    nothing about channels).
  - **`ac3`, `ec3`** (Dolby as it is; `mayDeclareDolby`: `!== 'off'`) — since 2026-09-30, after
    Safari 26.6.2 on a Mac played E-AC-3 copied on three files (1080p HEVC 10-bit; 4K HDR10 and 4K
    Dolby Vision 8.1, both with Atmos) and AAC 5.1 copied, once `4da7d00a` had fixed that browser's
    blob `<source>` (2026-09-29), and an iPhone did on 2026-09-30 06:30–07:07Z (every start checked
    in the transcoder's log: `session: audio audio=ac codecs=ec-3|ac-3`, the audio copied into
    fMP4): Chrome on iOS on hls.js — 1080p HEVC 10-bit with E-AC-3, 1080p HEVC with AC-3, 4K HDR10
    with E-AC-3 and Atmos; Safari on iOS on native HLS (`?mms=off`) — the two 1080p files; Safari on
    iOS on hls.js — the E-AC-3 1080p file, with a switch to another app and back for 30 s mid-play:
    it went on from where it was, no fallback. One fallback in all of it: the 4K file's second start
    in Chrome on iOS, a `decode_error` about 75 s in that nobody pinned (`audio-fallback cls=dolby
    by=unpinned`, the restart without Dolby), which the owner did not see — he had left the page by
    then; not explained (the app switch in Safari did not reproduce it). Before, only a browser that
    opened
    `?audio=on`. Who answers it (`codec-support`, sessions from 2026-09-28 21:10Z to 2026-09-29
    19:20Z): every Safari on a Mac (26 of 26) and every iOS browser (Safari 80/80, Chrome on iOS
    31/33, web views 8/8), Edge on Windows 32 of 40, Opera on Windows 8 of 15; Chrome on Windows 1
    of 255, on a Mac 0 of 33, on Android 1 of 108; Firefox none. The iOS answers are the native
    path's (`canPlayType`); since an iPhone plays through hls.js (above, 2026-09-30) it answers its
    ManagedMediaSource's `isTypeSupported` instead — not measured yet. Dolby is copied only into a
    passthrough's fMP4, so it reaches only HEVC sources with AC-3/E-AC-3 in those browsers.
    **Not verified**: AC-3 copied in Safari, anything on Edge or Opera. In upstream Chromium (main,
    read 2026-09-30) the answer follows the decoder: `MediaSource.isTypeSupported` →
    `HTMLMediaElement::GetSupportsType` → `MimeUtil::IsCodecSupported` →
    `IsDecoderSupportedAudioType` → `IsDecoderDolbyAc3Eac3Supported`, which on Windows and macOS reads
    the supplemental decoder cache, and on Windows that lists AC-3/E-AC-3 only where
    `DolbyDecMFT.dll` or the Dolby codec pack is there (`gpu_mojo_media_client_win.cc`); a build
    without `ENABLE_PLATFORM_AC3_EAC3_AUDIO` (Chrome) says no. Not read: Edge's and Opera's forks, and
    a decoder that is there and fails. A failure there that names no side is charged to `dolby` first
    (see "Dolby first, without an HEVC strike" below); what to watch
    is under "Watching the audio after the rollout". **Rolling Dolby back** is one line:
    `mayDeclareDolby` → `=== 'on'` (and its tests in `decode-declaration.test.js`).
  - **`?audio=off`** takes the browser out of every audio token: its pages send the video part
    alone, their probe asks nothing about audio (`declarationSupport(env, {audio: false})`: no
    `decodingInfo` and no `isTypeSupported` for audio, no `wt-decode-audio` written), `whenDeclared`
    waits for the video part only, and a cached audio answer from before is not sent. This is the
    viewer's (and support's) way out of multichannel audio: the stereo of old where 5.1 or Dolby
    does not play.
  `allowedAudioTokens` combines them: none where the page does not take part in the declaration at
  all, else `aac51` where `mayDeclareAac51`, `ac3`/`ec3` where `mayDeclareDolby` — today all three
  but for `?audio=off`. Where any audio token may go out the probe asks the whole audio part (Dolby's
  two questions are synchronous).
  The codec-support event is measurement, not the declaration: it asks every viewer the audio
  questions whatever the switch. Downstream everything keys on the declaration a start actually
  sent (`data-decode`, `DecodeRequest.DeclaresAudio`): the job's `data-audio-class`, the player's
  audio guard and the audio classes of the passthrough guard — a start without an audio token has
  none of them.
- **What** (`declarationFor(win, {resourceId, itemId})`), all or nothing:
  1. not taking part → no field;
  2. this file failed passthrough in this browser (memory below) → no field — its audio tokens
     included: the restart is the old route, stereo as it always was;
  3. the probe's video part answered → its tokens minus those the memory took away, then the audio
     part's that this browser may declare (`allowedAudioTokens`: every one it answered, none with
     `?audio=off`); none at all → no field (not an empty one);
  4. not yet (the `hdr-pq` answer is still out) → the cached answer of this same browser
     (`wt-decode`: same User-Agent, at most 30 days old); none → `unknown`, alone. HEVC tokens never
     go out without the `hdr-pq` answer: the transcoder would read the missing token as "no HDR" and
     refuse a 4K HDR film with a false reason. With `unknown` it plays ≤1080 the old way and answers
     a 4K start with "we were still checking — press Watch again" (`error.video_route.checking`).
- **The audio part** (`aac51`, `ac3`, `ec3`) is answered, cached and read apart from the video part
  and appended to it. Its one slow question (`aac51` over `decodingInfo`) has no deadline either;
  until it answers the part is this browser's cached audio answer (`wt-decode-audio`, same rules as
  `wt-decode`), else no audio token. That is not `unknown`: a missing audio token is only the stereo
  the transcoder has always made and cannot turn into a false refusal, while `unknown` is about the
  video and never goes out with audio tokens — the server would drop it for them
  (`models.ParseDecodeDeclaration`), and audio tokens alone read at the transcoder as "no HEVC".
  Neither part waits for the other: `startProbe`'s promise is the video part's (Discover waits on
  it, `usePlaybackContext`), `window.__wtDecode.audio` the audio part's (null where it was not asked).
  **`whenDeclared`** (the deep link's and the embed's 300 ms) waits for what the start sends: the
  video part's answer, and, where the audio part was asked, an audio part — the answer, or this
  browser's cached one (since 2026-09-29: the cache is what the declaration carries until the answer
  comes, so a browser that has one waits for no audio at all). Only a browser without a usable audio
  cache — its first page with the probe in 30 days, or its first after an update changed its
  User-Agent (Chrome's carries the major version: every four weeks) — waits for the audio answer, at
  most the same 300 ms, and side by side with the video part's `hdr-pq` question (`declarationSupport`
  asks both at once, so the wait added is only what the audio answer takes beyond the video's). What
  it takes: `decodingInfo` for the `aac51` configuration answered in 0.8 ms, the `hdr-pq` one in 1.8 ms,
  both together in 0.2 ms (Chrome 154 on a Mac, Playwright, 2026-09-29; Safari, Firefox and phones not
  measured). An answer that has not come within the 300 ms is no audio token: that start gets the
  stereo of old. Elsewhere `whenDeclared` waits for the video part alone. A browser that decodes no HEVC declares its audio
  alone (`aac51,ac3,ec3`); that is a declaration — its own job key, and a 4K refusal names the
  route's reason (docs/user_errors.md). A failed passthrough strikes no audio token; a failure of
  the audio has classes of its own (below).
- **Discover reads the video part only.** `decodedTokens` / `declaredTokens` are the video tokens,
  unchanged by the audio part's answer or its silence; `lib/discover/playback.test.js` pins that audio
  tokens move no switch even if they reached it.
- **Where.** A capture listener on `submit` (`installSubmitHook`, from `app/layout.js` before
  Turnstile) rewrites the hidden `decode` of every `/stream-video` form on every pass — idempotent,
  so Turnstile's two passes and either listener order end with the declaration of the moment. Starts
  made off the page (`background-render.js`: the next episode's cloned form, a settings restart)
  get it through `declare(body, form)`, which also drops a fallback's fields. A deep link
  (`#action=stream`, `app/resource/get.js`) waits up to 300 ms for the probe. The embed adds the
  field to its POST (`app/embed/check.js`), after the same 300 ms; there the file is not known yet, so
  only the per-class memory applies.
- **The memory of failures** (`wt-decode-fallback`, 7 days; `rememberFallback`): the file
  (`<resource-id>/<item-id>`) of a passthrough that failed — its next start sends nothing — and
  strikes against the decoder class that failed. Two strikes on **different** files within 7 days take
  the tokens that cover the class out of the declaration (`hevc8` → all four HEVC tokens, `hevc10` →
  `hevc10`, `hevc10-2160`, `hevc8-2160` → both `-2160`, `hevc10-2160` → itself); `hevc-high` and
  `hdr-pq` are never struck. Which failures strike: see the player's fallback.
  The **audio classes** go through the same memory (same key, same 7 days, same two strikes on
  different files), kept apart: a file whose multichannel audio failed is remembered under `audio`
  with its class, and its next start declares the rest — `dolby` leaves `ac3`, `ec3` out (the video
  keeps its route; the audio comes as AAC, 5.1 where `aac51` is declared), `aac51` leaves every audio
  token out (the stereo the transcoder has always made); two strikes take `ac3`+`ec3` (`dolby`) or
  `aac51` (`aac51`) out of every declaration (`AUDIO_DROP_BY_CLASS`, `AUDIO_STRUCK_BY_CLASS`). No
  audio class touches a video token, nor Discover's view (`decodedTokens`). A memory written by this
  build and read by an older one loses only the audio entries (the older one prunes classes it does
  not know).
- **State and storage.** Everything is on `window.__wtDecode` (the module is in several entries).
  Every localStorage access is caught: a browser whose storage throws keeps the switch and the memory
  for the page, and the layout's Turnstile and async navigation are never affected (tested with the
  real `app/layout.js` and a throwing getter).
- **Server side.** `handlers/action` and `handlers/embed` read `decode`, `decode-fallback` and
  `decode-class` through an allowlist (`models.ParseDecodeRequest`: the transcoder's token list and
  order — the six video tokens, then `aac51`, `ac3`, `ec3` — duplicates dropped, over 512 bytes →
  none). `unknown` goes only alone: a video token drops it (whatever answered is an answer); audio
  tokens beside it are dropped instead, because audio tokens alone read at the transcoder as a
  browser that answered "no HEVC" and would turn a 4K HEVC start into a false refusal (the page never
  sends the two together). The declaration is part of the job key (`DecodeRequest.Key`, appended only
  when present — a start without one keeps the id it had; `jobs/scripts/job_key_test.go` pins it to
  the ids recorded at `acf12dea`); a declaration of audio tokens only is one too, since its session's
  audio may be 5.1 or Dolby. It comes from the request, never the session: an account watches on
  several devices.
- **What a change of the declaration costs the job cache** (the audio's stage 5 appends `,aac51` to
  nearly every declaration, so every start's key changes once). A stream job's id is keyed by the
  viewer (`ApiClaims.SessionID` / user id), the file, the declaration and a **10-minute time bucket**
  (`jobs/scripts/action.go`, `cacheKey`; the embed's by the hour, `jobs/scripts/embed.go`); its log
  lives in Redis for the job's 30 minutes. So the cache only ever serves a viewer who starts the same
  file again within the same bucket, and a key change can only miss those repeat starts that straddle
  it: at most one bucket's worth of hits, once — what every bucket boundary already does to every
  repeat start 144 times a day. Production, read-only (Prometheus, 2026-09-22…29): `POST
  /stream-video` answered 200 vs `webui_jobs_total{job="stream-video"}` runs — 6675 vs 5607 on
  2026-09-27, 5215 vs 4097 in the day to 2026-09-29 16:00Z, i.e. 16–21% of starts (≈1 070–1 120 a
  day) were served by the cache or joined a job in flight; per 10 minutes ≈7 on average, 13 at the
  90th percentile, against ≈31 runs (81 at most). The embed: 1388 `POST /embed` 202 vs 1286 runs a
  day, ≈4 hits an hour, 16 at most. **The wave is therefore ≤ ~13 extra stream-job runs and ≤ ~16
  extra embed runs, once** — and less, because the new declaration reaches a viewer only with the
  next full page load (the layout's JS is the page's until then), which spreads it over hours rather
  than one bucket. The HEVC stage 5 was the same change to every key (web-ui `sha-ef1e89a`, deployed
  2026-09-28 16:10Z): the cache's share was 19%, 16%, 22% in the three 10-minute windows after
  it, 6–29% in the five before: no dip to see. No code avoids it without weakening the key, and none is
  needed: a render for a declaration with `aac51` holds a session whose audio is 5.1, which must not be
  served to a start without it. Not measured: what the transcoder's own reuse of FFmpeg runs between
  a stereo and a 5.1 session of the same file costs (content-transcoder's side).

Reading the tokens: the share that matters for 4K is `hevc10-2160` (93% of >1080p HEVC sources are
Main10), and `hevc10-2160` with `hdr-pq` for PQ (52.5% of them); split by `decode_path` and by
browser. Note that hls.js 1.6.14 itself asks `decodingInfo` for every HEVC level and drops a level it
calls unsupported — but only when the master has more than one level
(`abr-controller.ts`, `removeLevel` guarded by `levels.length > 1`).

#### The session's route — `data-video-route`

The transcoder answers POST `/session` with the route it chose (`video_route`: `passthrough`, `copy`,
`reencode`, `audio`; `route_reason`: `ok`, `no_declaration`, `passthrough_off`, … —
content-transcoder `docs/session-transcoding.md`), and a refusal (415, or 503 for a source check that
did not answer) with the reason in `X-Video-Route-Reason` (`api.TranscoderRefusal`; its `Error()` is
the text the untyped error had, so `ClassifyError` and the logs read it as before). The stream job
(`jobs/scripts/hls.go` `bufferSessionHLS`, `passthrough.go` `applySessionRoute`) puts on the
`<video>`, each only where it is known — without them the tag is byte for byte what it was:

| attribute | what |
|---|---|
| `data-video-route`, `data-route-reason` | the session's route and its reason (a transcoder that predates routes sends neither) |
| `data-decode` | the declaration this start sent (`vsud.Decode`), for telemetry |
| `data-video-class` | passthrough only: the decoder class the video needs — `hevc8`, `hevc10`, `hevc8-2160`, `hevc10-2160`, `unknown` — read from the master's `CODECS` (the transcoder writes them from the init it produced, not from the source's record, which FFmpeg rebuilds) and `RESOLUTION`, by the transcoder's own rule: profile 2 is 10-bit; taller than 1080, wider than 1920 or level over 4.1 is 2160. The class a failure in the browser is charged to |
| `data-frag-load-ms` | passthrough only: how long one segment may load, `max(120 s, 2 × BANDWIDTH × target duration / cap)`, at most 15 min (`passthroughFragLoadMs`; no cap → 120 s; a master without a real BANDWIDTH → the file's rate) |
| `data-audio-class` | any route, only for a start that declared an audio token: what that declaration made of the session's audio, read from the master (`sessionAudioClass`) — `dolby` where a variant's `CODECS` names `ac-3`/`ec-3` (or `mp4a.a5`/`mp4a.a6`), else `aac51` where an audio rendition has `CHANNELS` over 2; absent for the stereo AAC the transcoder has always made (whose master says neither). The audio class a failure is charged to where hls.js does not say better |
| `data-item-id` | where the player may restart the file by itself (`StreamContent.PlayerRestarts`): a passthrough, or a start that declared an audio token |

For a passthrough the transfer status's marks are made again once the route is known
(`setRoutedStatusMarks`): before the session a transcoded HEVC counts as re-encoded, whose rate is
unknown; passed through it is pulled at the source's own rate (`playedBitrateRouted(…, videoCopied)`),
so the cap card and "this file needs N" work as for H.264. Every other caller of `playedBitrate` is
unchanged, and the cap gate before the session still falls back to the file's rate for HEVC —
conservative, and right for a passthrough.

#### An MP4 whose audio nginx-vod cannot hand the browser — `jobs/scripts/vod_route.go`

rest-api sends a file to nginx-vod or to the transcoder by its extension alone: an `.mp4` goes to
nginx-vod (`~vod/hls/<id>/index.m3u8`), which serves its first video and first audio track as they are,
muxed. Where the browser cannot take that audio it played nothing, or played without sound, with no error
the viewer sees: hls.js cannot add the SourceBuffer (`video/mp4;codecs=ec-3,hev1…` → `NotSupportedError`,
a fatal `bufferAddCodecError`) and the old route's handling recovers it every ~110 ms for as long as the
page is open (2026-09-30, a 2160p HEVC MP4 with E-AC-3 in Chrome 154; 7 `player-dead why=recovering` on
vod in 12 h). After the probe the stream job sends such an MP4 to the transcoder instead — the vod URL's
file with `~hls/index.m3u8`, the path cut, not re-encoded — and the transcoder encodes the audio as the
declaration allows (the repro's file: HEVC passed through, AAC 5.1, both audio tracks; it plays).

What nginx-vod serves (nginx-vod-module `26f06877`, the one the image builds):

- **the first video and the first audio track it counts** (`v1-a1`). For HLS it counts H.264, HEVC and AV1
  video, and AAC, MP3, AC-3, E-AC-3, FLAC and DTS-in-`mp4a` audio (`mp4_parser.c`, `ngx_http_vod_hls.c`
  `SUPPORTED_CODECS`). A track in any other codec — VP9, VP8, Opus, ALAC, PCM, TrueHD (`mlpa`), DTS in
  `dtsc`/`dtsh` — is skipped and takes no index, so `a1` is the next audio track. The viewer's pick does
  not change it: the pick is the job's key and the list's default, not the stream's;
- **fMP4 when its first track is a video that is not H.264, MPEG-TS otherwise**
  (`ngx_http_vod_hls_get_container_format`, container `auto`). In fMP4 the browser's MSE decodes the
  audio. In MPEG-TS hls.js demuxes, and nginx-vod writes E-AC-3 under AC-3's stream type (`0x81`,
  `mpegts_encoder_filter.c`): a browser without AC-3 drops it (silence), one with AC-3 has E-AC-3 frames
  fed to the AC-3 parser. E-AC-3 in TS plays nowhere.

| `a1` (the first audio track nginx-vod counts) | goes to the transcoder | `webui_vod_reroute_total{reason}` |
|---|---|---|
| E-AC-3, container MPEG-TS (H.264, or no video nginx-vod serves) | always | `eac3_ts` |
| E-AC-3, container fMP4 | where the start does not declare `ec3` | `eac3` |
| AC-3 | where the start does not declare `ac3` | `ac3` |
| DTS | always: no browser decodes it | `no_decoder` |
| none — the file has audio, nginx-vod serves none of it | always: silence on nginx-vod | `unserved_audio` |

A start without the token counts as a browser that cannot play it, whatever the reason: no
declaration, `unknown`, video tokens only (`?audio=off`, or the audio answer not in yet), a restart after a
failed passthrough or in compatibility mode (the page sends no declaration), the page's 7-day memory of
one. Most browsers cannot (Chrome, Firefox), and a start without the token is most often a file that
already failed there — reading it as "unknown, keep vod" put a fallback restart straight back on
nginx-vod's dead player.

What it costs:
- a browser that could play Dolby but did not say so (Safari on its first page, before its audio answer;
  `?audio=off`) gets AAC from the transcoder instead of Dolby from nginx-vod; a 2160p HEVC the transcoder
  cannot pass through (HLG, Dolby Vision 5) gets its refusal where nginx-vod had played it in Safari;
- `eac3_ts` also takes a browser on native HLS (iOS before 17.1, `?mms=off`) off nginx-vod, where native
  HLS played it. On the transcoder that H.264 file takes the old route, and Dolby is copied only into a
  passthrough's fMP4, so that viewer hears AAC 5.1 instead of Dolby;
- DTS stored as `dtsc` (which ffprobe does not tell from DTS in `mp4a`) goes to the transcoder where
  nginx-vod may have served the next, playable track;
- every reroute is an FFmpeg run (the video copied, the audio encoded) where nginx-vod needed none. Hold a
  day of `webui_vod_reroute_total` against the transcoder's CPU after the rollout.

The job key already carries the declaration, and its 10-minute bucket lets a render made before the change
go. Not handled: a video codec the browser lacks on nginx-vod (an HEVC MP4 in a browser without HEVC) —
unmeasured; the player's endless recovery of a fatal codec error on the old route (`hls-manager.js`) is
left to `player-dead` to count.

#### Passthrough: errors and fallback — `passthrough.js`

Only where the session's route is `passthrough` (`data-video-route`); every other stream's error
handling is what it was (`hls-manager.js`: every fatal media error recovers, network errors restart
loading) — but for a start that declared multichannel audio, whose own guard is below ("Multichannel
audio and the fallback"). The player makes a guard (`createPassthroughGuard`) that sees hls.js's
errors first and listens to the element, and gives the file up **at most once per player**; where
the failure is the audio's the class it is charged to is an audio one and the restart keeps the
video's declaration (below):

| signal | reason | strikes the class |
|---|---|---|
| hls.js `manifestIncompatibleCodecsError`, or a fatal `bufferAddCodecError` (the init's codec; with one level hls.js makes it fatal before any listener) | `codecs_rejected` | no — a build fault of ours; `webui_passthrough_fallback_total{reason="codecs_rejected"}` alerts |
| a fatal media error (hls.js `MEDIA_ERROR`: `bufferAppendError`, `fragParsingError`, …) or, on the hls.js path, the element's own `error` with MediaError 3 — hls.js does not listen for it and learns of it only at its next append, which with a full buffer can be half a minute or never. The first is recovered (`recoverMediaError`); the next, for the rest of this playback, gives up. Two reports within 1 s are one incident — except that a report swallowed so after the recovery is looked at again when the second ends: an element holding an error then is playing the attachment that failed (below) | `decode_error` if the element said MediaError 3, else `media_error` | only `decode_error` |
| native HLS (no hls.js: iOS, or no MSE): the element's `error` — 3 | `decode_error` | yes |
| — 4 (Safari may say it for a master that failed to load too; not verified) | `src_unsupported` | no |
| the watchdog, native and hls.js: 10 s after the first `playing`, in a tab that stayed visible, time ran on by more than 2 s and there is no picture — `videoWidth` 0, or 0 decoded frames where this page has seen the counter count (Android Chrome's native player reads 0 while it plays) | `no_frames` | yes |
| "Compatibility mode" (below) | `user` | no |
| the same fragment loaded again and again, where the old route plays the file (see "A fragment loaded again and again") | `fragment_loop` | no |

**One incident, or the next one at once** (`createIncidents`, since 2026-09-28). Before, a report
within 1 s of the recovery was always taken for the first incident told twice — but hls.js stops
loading on a fatal error, so where it was really the recovered attachment failing at once (a
fragment fetched again from cache fails in a few hundred ms) nothing else came, and the player sat
on a dead MediaSource without falling back. Now such a report is looked at again at the end of the
second: the recovery reloads the element (hls.js `detachMedia` calls `load()`), which by the HTML
load algorithm clears its `error` and drops its queued `error` events, so an element that holds an
error then is on the attachment that failed — the next incident. A clean element leaves it one
incident, as before. Tested with an hls.js stand-in whose recovery clears the element; not verified
in a real browser.

Network errors are not a fallback. `fragLoadPolicy` for passthrough: `maxLoadTimeMs` from
`data-frag-load-ms` (twice the segment's time at the viewer's cap, 2–15 min), a timed-out segment tried
3 times in all (`timeoutRetry` 2, where today's legacy settings make it 100 refetches from zero), HTTP
errors retried as today (`errorRetry` 100, 1–10 s; a test reads today's value off a real hls.js
instance). After the tries hls.js raises a fatal network error and `hls-manager.js` restarts loading, as
on every route — **without a limit**: hls.js counts fragment errors until the next successful load, so
each further timeout is fatal at once. The loop costs one download attempt per `maxLoadTimeMs`; it is
not a finite number of retries.

The fallback (`fallbackToOldRoute`):

1. the memory (`decode-declaration.js`): the file, and a strike against its class for the reasons above;
2. Umami `hevc-fallback {reason, cls, path}`;
3. a visible restart from the start (the position is v1.1), on the old route:
   - **embed** (`window._embedSettings`): its POST again — settings, `decode-fallback`, `decode-class`,
     no `decode` — not a reload, which would send the old body, `decode` included, again;
   - **the page's start form is this file's**: the restart note on the page (`setPendingFallback`), the
     fields put on the form, `requestSubmit()` — the button's path, Turnstile included. The note makes
     the hook write `decode-fallback`/`decode-class` and no `decode` on each of Turnstile's passes, and
     ends when the next player comes up (or after 2 min): a field left on the form would put `/fb=` in
     the key of a start that restarted nothing and count a fallback that did not happen. Background
     starts (`background-render.js`) never carry them;
   - **the form is another file's** (the player moved on to the next episode quietly and the page is
     not brought up to date yet — in fullscreen until it ends): this page for this file, started by its
     deep link, `#action=stream&decode-fallback=…&decode-class=…` (`app/resource/get.js` sets the note).
     Restarting the page's form would restart the previous episode. The quiet move has already pushed
     `?file=<that file>` into the address, so the deep link differs from it only by the hash — a
     fragment navigation, which loads nothing and leaves the viewer on the failed player.
     `loadDocument` replaces the address and reloads there (`fallback-navigation.test.js`);
   - no form at all: a reload.
4. The server (`handlers/action`, `handlers/embed`) counts `webui_passthrough_fallback_total{reason,
   class}` and logs `passthrough fallback`. A file ≤1080 plays on the old route; over 1080 the
   transcoder refuses it and the viewer reads `error.video_route.fallback_uhd`
   (docs/user_errors.md, "Route refusals").

The class is `data-video-class`, read by the job from the master's `CODECS` (see "The session's
route"). Not verified in a real browser: which errors hls.js and each browser raise when HEVC decoding
fails, the watchdog on iOS, `requestSubmit()` through a visible Turnstile checkbox mid-film — the
stage 4 matrix.

#### Multichannel audio and the fallback

A start that declared `aac51` / `ac3` / `ec3` may get audio no browser was handed before: AAC 5.1
(copied, or encoded to instead of stereo) on either route, and on a passthrough Dolby copied as it
is. When that audio fails, restarting the file with no declaration at all — the video's fallback — is
wrong twice over: on a passthrough the restart is the old route, which refuses a 4K HEVC film
("This browser couldn't show this 4K HEVC video", though it played before multichannel audio, with
stereo), and the failure strikes the video class, so two such files take HEVC out for 7 days. So a
failure is charged to an **audio class** where it is the audio's, and the restart leaves only that
audio out (`fallbackAudio`).

**What the audio is** — the class a failure can be charged to: what hls.js buffers first
(`BUFFER_CODECS`, `audioOfTrack`: `ec-3`/`ac-3` parsed from the fMP4 init → `dolby`; an MPEG-TS
AAC whose ADTS says more than two channels → `aac51`, and one that says 0 — the layout is in a PCE,
hls.js builds its AudioSpecificConfig from that header and the browser cannot place the channels —
also `aac51`; one or two channels → the stereo of old, no class), else the master's word
(`data-audio-class`: native HLS, and a failure before any audio was buffered). An fMP4 AAC track
carries no channel count, so there hls.js says only that the audio in play is not Dolby: it counts as
AAC 5.1 where the master names changed audio (a master with a Dolby rendition may have an AAC 5.1
one beside it), else as the stereo of old — never as the master's Dolby, which is another rendition.
A master that labels nothing and a track that says nothing give no class: the rules before.

**Which side failed** (`fault`): hls.js names the SourceBuffer whose own append failed
(`bufferAppendingError`, or a fatal `bufferAddCodecError`) — kept for the incident that follows
within 30 s and forgotten at the recovery; else the element's MediaError message where it names
only one of `audio` / `video` (`messageSide`; browser text, not a standard). A decoder that fails
after its append names no buffer: hls.js's later `bufferAppendError` is named after whichever buffer
appended next, so it is not read.

What the messages say, from the browsers' sources (Chromium, WebKit and Firefox main, read
2026-09-29) and, where marked, seen in headless Chrome 154.0.8037.58 on a Mac the same day:

- **Chromium** (Chrome, Edge, Opera): `<PipelineStatus>: <the first error its media log saw>`
  (`content/renderer/media/batching_media_log.cc`, `GetErrorMessageLocked`); Chrome 154 puts the
  group before the code — seen: `PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop:
  stream parsing failed. …` (the PCE stand, no side named) and `PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED:
  Unsupported audio format 0x65632d33 in stsd box.` (an fMP4 whose E-AC-3 init Chrome cannot take:
  the audio named). Decoders name their
  stream: `PIPELINE_ERROR_DECODE: audio decode error!` (`decoder_stream.cc`), `…: Failed to send audio
  packet for decoding: …` (`ffmpeg_audio_decoder.cc`), `DECODER_ERROR_NOT_SUPPORTED: audio decoder
  initialization failed with …`. **An audio output that goes away** under a playing stream —
  Bluetooth headphones disconnecting, a USB DAC unplugged — is the audio sink's render error:
  `AudioRendererImpl::OnRenderError` logs `audio render error` and fails the pipeline with
  `AUDIO_RENDERER_ERROR` (`media/renderers/audio_renderer_impl.cc`), which is a MediaError **3**
  like a decoder's (`web_media_player_impl.cc`, `PipelineErrorToNetworkState`): message
  `AUDIO_RENDERER_ERROR: audio render error` (in Chrome 154, by the prefix seen above,
  `PipelineStatus::AUDIO_RENDERER_ERROR: …`; not reproduced — headless Chrome has no output device to
  lose). YouTube shows the same failure as "Audio renderer
  error. Please restart your computer", which support pages tie to headphones being plugged or
  unplugged. It names the audio, but not the audio the declaration changed, so `messageSide` reads
  any message with `AUDIO_RENDERER_ERROR` as naming no side (since 2026-09-29; before, it was taken
  for the audio failing: a restart and an `aac51`/`dolby` strike). On the old route that failure is
  now `hls-manager.js`'s, recovered as on every stream (the recovery attaches afresh, on whatever
  output is there). The passthrough guard still reads its MediaError 3 as a decoder failure — the
  video's, `decode_error`, a strike of the HEVC class on the second one within a playback; that is
  the video's rule and is left to its owner.
- **Safari** (WebKit): `Media failed to decode` / `Media failed to load`, nothing after it — no
  player gives WebKit an `errorMessage` (`HTMLMediaElement.cpp`, `MediaPlayerPrivate.h`). Its MSE
  renderer turns every `AVSampleBufferAudioRenderer` error into `AudioDecodingError` and that into a
  plain decode error (`AudioVideoRendererAVFObjC.mm`, `MediaPlayerPrivateMediaSourceAVFObjC.mm`), so
  Safari never names a side, and an output change there is a flush the renderer handles
  (`audioRendererWasAutomaticallyFlushed`), not an error.
- **Firefox**: `NS_ERROR_DOM_MEDIA_… (0x…) - <function>`; its audio sink's failure
  (`NS_ERROR_DOM_MEDIA_MEDIASINK_ERR - OnMediaSinkAudioError`, a text with "Audio" in it) is raised
  only for media without video (`MediaDecoderStateMachine::OnMediaSinkAudioError`: with video it
  plays on), so it never reaches a guard.

**The rule** (`audioFallbackClass`), for every guarded failure but `no_frames` (no picture is the
video's) and `user`:

| audio | fault | charged to |
|---|---|---|
| `dolby` or `aac51` | `audio` | its class |
| `dolby` | nobody | `dolby` — "Dolby first" (`by: 'unpinned'`) |
| `aac51` | nobody | the video's, as for a session whose audio the declaration did not change |
| any | `video` | the video's |
| none | any | the video's |

`audio` here is the audio in play (`createAudioSide`, `audio()`): hls.js's buffered track, the
master's where nothing is buffered yet and on native HLS — a Dolby master whose AAC rendition is in
play is `aac51`.

**"Dolby first", without an HEVC strike** (again since 2026-09-30). A failure nobody pinned on a side, in a
session whose audio is Dolby, is charged to `dolby`: the restart goes without `ac3`/`ec3` and keeps
the HEVC route (a 4K film stays a passthrough), and it is Dolby that is struck, never HEVC. Umami
`audio-fallback` then carries `by: 'unpinned'` — the charge was the rule's, not the browser's word.
The two wrong answers do not cost the same, and the cheap one goes first:

- a false `dolby` strike (the video was at fault) costs this browser Dolby for 7 days — the audio
  comes as AAC 5.1 instead, and the start without Dolby fails again, so the video's rules take it
  from there: one extra restart, then the old route and, for `decode_error`, the HEVC strike it
  would have had anyway (on the second file, as always);
- a false HEVC strike (Dolby was at fault) costs it passthrough and 4K for 7 days, and a Dolby
  failure that is systematic on some device would take its HEVC classes out one by one — two files
  per class — while the cause stayed in the declaration.

The ambiguous case is the common one where Dolby is declared most: WebKit names no side in any
MediaError (Safari and every iOS browser; its MSE renderer turns every audio renderer error into a
plain decode error). Of the 28 `hevc-fallback` events from 2026-09-28 21:30Z to 2026-09-29 19:30Z, 17
were native `decode_error` in WebKit — 13 on iPhones and iPads (9 Safari, 3 Chrome on iOS, 1 iPad in
desktop mode) and 4 Safari on a Mac — and 5 more `src_unsupported` on iPhones (Umami, recounted
2026-09-30), all before any Dolby was declared there. A passthrough on iOS falls back in about a
quarter of its attempts (`hevc_fb / (pt_starts + hevc_fb)`, the day to 2026-09-30 00:30Z: `ios` 15 of
64, `crios` 6 of 22; Safari on a Mac 4 of 22), and about 44% of HEVC sources carry AC-3/E-AC-3 (the
stage-5 review's count over content-prober's probes, 24 h to 2026-09-30: E-AC-3 first in 118 of 331,
AC-3 in 27). So a real HEVC failure on a file with Dolby now costs one extra restart, and for
`decode_error` a `dolby` strike on top of the HEVC one; two such files in 7 days and that browser
plays Dolby as AAC 5.1 for a week. That is the price accepted for never losing 4K to an audio
decoder. Chromium names the audio's decoder in its messages (`audio decode error!`, `audio decoder
initialization failed …`, from its source, not seen), so there the charge is mostly on evidence.

A refused manifest (`manifestIncompatibleCodecsError`) with Dolby in its `CODECS` goes the same way:
hls.js drops every variant one of whose `CODECS` its MediaSource refuses, so it names no side either,
and the restart without Dolby settles it.

AAC 5.1 stays evidence-only: a false `aac51` strike takes every multichannel token (stereo for 7
days), dearer than a restart, and nothing marks AAC 5.1 as the likely failure — the owner played it
in Chrome and Safari without one.

History: this was the rule until 2026-09-29; from 2026-09-29 to 2026-09-30 the branch charged the
unpinned case to the video ("No Dolby first"), to spare a real HEVC failure the extra restart and the
false `dolby` strike; the stage-5 review showed that a Dolby failure systematic on some WebKit device
would then have taken its HEVC classes out one by one, and the rule went back. The unmeasured world is still a Dolby decoder failing anywhere: no
production start had declared Dolby before this rollout (0 of the 3 114 `stream-start` events that
carry `decl`, 2026-09-28 to 2026-09-30 00:40Z, have `ac3`/`ec3` in it), and the owner played it only in
Safari on a Mac. Since 2026-09-30 an iPhone plays through hls.js (above): there an init its parser
refuses is pinned on the audio SourceBuffer — evidence — but a decoder failing later still names no
side, and the iOS rates above were measured on native HLS and are to be measured again.

What closes it:

- **Before the rollout** (the owner's devices, done 2026-09-30, see "Who declares audio"): an iPhone
  played HEVC passthroughs with E-AC-3 and with AC-3 on hls.js and on native HLS (`?mms=off`). Not
  done: AC-3 in Safari on a Mac, anything on Edge or Opera.
- **24 h after**: the query under "Watching the audio after the rollout". Roll Dolby back
  (`mayDeclareDolby` → `=== 'on'`) where a browser's `dolby` passthroughs fall back to the audio
  (`pt_audio_fb` with `by = 'unpinned'`) or to the video more than twice as often as its `none`
  ones fall back at all, or die more often (`pt_dead`). A rollback leaves the `dolby` strikes
  already written (localStorage `wt-decode-fallback`, 7 days) until they expire; they only drop
  `ac3`/`ec3`, which a rollback drops anyway.

**The restart** (`fallbackAudio`): the memory (the file's audio class; a strike for `decode_error`
and `media_error` — `AUDIO_STRIKING`, wider than the video's because an append the browser refused
is what the declared capability failing looks like on the audio side, and a struck audio class costs
stereo or AAC-instead-of-Dolby for 7 days where a struck video class refuses 4K), Umami
`audio-fallback {reason, cls, path, route}` (not `hevc-fallback`, whose count stays the video's),
and the same restart as the video's — embed POST (with the `decode` it keeps), the page's form, the
deep link, a reload — carrying `decode-fallback` and `decode-class=dolby|aac51`. The declaration of
that start is the rest: `dolby` → without `ac3`,`ec3` (a 4K HEVC film stays a passthrough and plays),
`aac51` → without any audio token. The server allows the two classes
(`models.ParseFallbackClass`), counts them in `webui_passthrough_fallback_total{class}`, and does
not mark a refusal of such a restart as the video's fallback: it still declares its video, so a 415
there is the route's own word, not "this browser could not show it" (`DecodeRequest.IsAudioFallback`).

**The old route** (MPEG-TS; `createAudioGuard`): only a start that declared an audio token gets a
guard (`declaresAudio(data-decode)`) — since the audio's stage 5 for AAC 5.1 that is nearly every
start; a browser opted out (`?audio=off`) runs exactly the old handling. The guard restarts a file without its audio tokens (charged to `aac51`:
on the old route that is the only class that can happen, Dolby is copied only into fMP4) **only for a
failure that is the audio's, and never for two failures with the audio shown working in between**
(since 2026-09-29; before, any two fatal media errors of a session — the audio's or not, an hour
apart — gave the file up, and on iOS the first element error did):

- **hls.js** (every browser, iPhones and iPads too since 2026-09-30 — `iosPlaysHlsJs`): what counts
  is a media error **pinned on the audio** —
  hls.js 1.6.14 names the SourceBuffer whose own append failed (`bufferAppendingError`, raised from
  that buffer's `error` event, i.e. the MSE append error algorithm for its data;
  `buffer-controller.ts` `onSBUpdateError`) or whose codec the browser refused (a fatal
  `bufferAddCodecError`), kept for 30 s (`FAULT_TTL_MS`); else the element's MediaError message where it
  names the audio alone. The fatal `bufferAppendError` itself is not read for the side: once the
  MediaSource has ended in error, hls.js raises it for whichever buffer appended next
  (`appendBuffer` throws, `onError` of that buffer's operation). A fatal one is recovered by the
  guard. A **non-fatal** `bufferAppendError` after the pin is one of two things, told apart by the
  message the SourceBuffer's error gave it (`recoveredByHls`). With `MediaSource readyState: ended`
  hls.js has recovered it by itself: its error controller calls `recoverMediaError` for a resolved
  append error of an ended MediaSource (`error-controller.ts` `onErrorOut`), before any listener of
  ours runs — that counts as the one recovery, and the guard makes none on top of it. With
  `readyState: open` (the SourceBuffer's `error` event came before the MediaSource ended) **nobody
  recovers it**: content steering marks every non-fatal `bufferAppendError` resolved
  (`content-steering-controller.ts` `onError`; with several levels a level switch does), so it
  never turns fatal, and `onErrorOut` recovers only `ended`. The element is left in MediaError 4 and
  no further error comes — the guard recovers it itself, as a fatal one. Each pinned report
  is a failure of its own (the recovery forgets the pin), so there is no same-incident window here.
  The next failure gives the file up — unless, since that recovery, **30 s of media played**
  (`CLEAN_PLAY_S`: forward `timeupdate` steps of at most 2 s, so a seek or the recovery's own reload
  is not playback) or **5 minutes passed** (`RELATED_MS`: a viewer who paused shows nothing either
  way); then it is a first failure again, recovered. A fatal media error nobody pinned on the audio —
  a video decoder, a segment that did not parse, a MediaError message naming no stream — is
  `hls-manager.js`'s, which recovers it as on every old-route stream.
- **Native HLS** (iOS with `?mms=off` or without a ManagedMediaSource, before 17.1): nothing names a
  side — Safari's MediaError messages are empty and there is no
  SourceBuffer — and nothing recovers: an element in error stays dead (without a guard that is what
  every native stream has always done). So the guard acts only **before the stream has played 30 s of
  media** (the same clock, counted from the player's start): MediaError 3 → `decode_error`, 4 →
  `src_unsupported`. Why this and not "no restart on native at all": the restart goes from the start —
  nothing lost for a viewer who is there, the position lost for one who seeked or resumed far into the
  film before 30 s had played (restarting at the position is a possible later step; the alternative
  today is a dead element), the audio is what the declaration changed at the start of
  an H.264 stream, and without it a device whose AAC 5.1 fails would get a dead player on every
  multichannel file with no memory to stop declaring it (`aac51` on native is `canPlayType`, which
  says nothing about channels). After 30 s the audio has decoded on this device, and an error is the
  old route's (no restart). A message that names the video alone keeps it the video's. What it costs
  where the failure was not the audio's: one extra restart, the file's stereo for 7 days, and for
  `decode_error` a strike (two files → `aac51` out of this browser's declaration for 7 days).

The measured case is AAC whose layout is in a PCE, copied for `aac51` (a stand: in production the
transcoder encodes such a track, see the known gap below): Chrome refuses the audio append
(hls.js `bufferAppendingError` on the audio buffer, then a `bufferAppendError`; MediaError 4,
`PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed`) and
every recovery fails the same way before anything plays. **Reproduced in a real browser on
2026-09-29** (Chrome 154 through Playwright, hls.js 1.6.14, MPEG-TS HLS made with
`ffmpeg -aac_pce 1`, ADTS channel configuration 0 in every frame; controls with standard 5.1, channel
configuration 6, played with no error, ~5.8 s of media in 6 s) on two shapes of stream, which fail
differently:

- **the old route's master as content-transcoder writes it** (an `EXT-X-MEDIA TYPE=AUDIO` rendition
  beside the video level, content-transcoder `services/testdata/golden_old_route.json`): the first
  failure is mostly non-fatal with `readyState: open` — nobody recovers it, the element holds
  MediaError 4, and nothing follows. **Every guard before 2026-09-29 left a dead player there**: no
  restart, no memory, no `audio-fallback` (found by the stage-5 reviews: the base 4 of 4 runs, the
  first stage-5 version of this guard 8 of 8, and 2 of 2 again here). In some runs the first failure came fatal instead (a
  race with the element's error; 1 of 8 here). With the `open` failure recovered by the guard it
  gave up after its one recovery, 232–698 ms after the start (8 of 8 runs, with and without 200 ms
  of delay per segment);
- **a muxed media playlist** (one TS with both streams, no master): every failure after the first
  recovery is non-fatal with `readyState: ended` and recovered by hls.js itself, ~1 000 times a
  second. The guard before this change never gave up there (1 662 recoveries in 5 s, no restart — its
  test stood in a fatal error for each), and neither does the passthrough guard (below). Counting
  hls.js's own recoveries, it gives up after the second, 123–127 ms after the start (earlier runs:
  16 ms after the first failure), and the loop stops.

On giving up the audio guard stops hls.js (`stopLoad`, `detachMedia`), since hls.js's own recovery
re-attaches until the restart replaces the player. The pin alone decides (the MediaError message names
no stream), and a seek in between is no clean playback (`passthrough.test.js`, "the PCE case", "a
failure hls.js recovered by itself", "a non-fatal append error hls.js did not recover"; a test pins
the installed hls.js source: the one `recoverMediaError` of `onErrorOut` under `ended`, content
steering resolving a non-fatal `bufferAppendError`, `onErrorOut` registered before the application's
listeners). Everything else on such a stream is the old route's: a failure pinned on the video,
audio the declaration did not change, the element's errors on the hls.js path (hls.js learns of them
at its next append) — `hls-manager.js`, as it always has. The
passthrough guard (`createPassthroughGuard`) keeps its own rule: a session's second media failure
gives the file up whenever it comes.

**Known gap, not fixed here: the passthrough guard does not see non-fatal append failures.** It counts
only fatal media errors (and, on the hls.js path, the element's MediaError 3, whose events the
recovery's reload drops). A SourceBuffer append that fails non-fatally — audio or video — slips past
it in both shapes above: with `ended` hls.js recovers by itself and the guard never reaches its second
failure (the muxed PCE stream on a passthrough guard: 1 719 hls.js recoveries in 5 s, no fallback);
with `open` nobody recovers, and a MediaError 4 on the hls.js path is not the guard's either (it reads
the element's 3 only) — a dead player with no fallback (the reviewers' MPEG-TS stand with an audio
rendition). **Measured on the passthrough's own shape** (2026-09-29, headless Chrome 154, hls.js
1.6.14, this guard as it is): an fMP4 HEVC video with its audio as a rendition of its own, the
master saying `mp4a.40.2` and the audio init being E-AC-3 — what content-transcoder served once
after a copied E-AC-3 fell back to AAC (`transcode_run.go`, `dropChangedAudioPlaylistsLocked`, fixed
there) — gets the `open` shape: `bufferAppendingError` on the audio buffer, a non-fatal
`bufferAppendError`, MediaError 4 `…Unsupported audio format 0x65632d33 in stsd box.`, and nothing
after it: no fallback, the player at 0 s (2 of 2 runs, under either rule for an unpinned failure). The
controls: the same video with AAC stereo plays (5.6–5.8 s of 6 s); with the master saying `ec-3`
Chrome refuses the variant and the guard gives up at once (`codecs_rejected`, 7–12 ms, charged to
`dolby` — "Dolby first"). With Dolby declared by default the trigger is a
master and an init that disagree about the audio — or a browser whose MediaSource takes a codec its
parser then refuses; neither is known to happen now. The second one is reachable on the Dolby master
itself (`CODECS="hvc1…,ec-3"`, the audio a rendition of its own): with `isTypeSupported` and
`addSourceBuffer` of the bench faked to take `ec-3` and parse it as AAC, Chrome 154 gives the same
`open` shape — `bufferAppendingError` on the audio buffer, a non-fatal `bufferAppendError`, MediaError
4 `…Unsupported audio format 0x65632d33 in stsd box.`, no fallback, the player at 0 s (2 of 2 runs in
the review, 6 of 6 here, 2026-09-30). Unlikely in practice: in upstream Chromium the `ec-3` answer
follows the decoder being there (see "Who declares audio"), and WebKit parses `ec-3`. The audio
guard's changes (count hls.js's own
recovery, recover an `open` one itself, stop hls.js on giving up) would close it; it is the HEVC
passthrough's guard, so it is left to its owner. **`player-dead` does not report it either**: the
element errs before it has metadata and Chrome pauses it (`error`, then `pause`, in the same
millisecond), and the watch takes any `pause` for the viewer's and disarms (`dead-player.js` `onPause`).
The same bench with the watch as it is on `main` (plus `audio`): no `player-dead` in 14 s with an 8 s
quiet (3 of 3) or in 40 s (1 of 1); with a bench-only rule "a `pause` while the element holds an error
does not disarm", `player-dead {why: error, audio: dolby}` 9 s after the start (2 of 2). The PCE stand
on the old route's master — the case the `why: error` rule was written for — pauses the same way and
reports nothing either (2 of 2; there the audio guard restarts it now). That rule is the `player-dead`
owner's and is not changed here. Since the audio's stage 5 every passthrough of a
multichannel source carries AAC 5.1 — but content-transcoder copies only AAC whose layout ffprobe
names after a configuration (3.0, 4.0, 5.0, 5.1: `services/audio.go` `aacConfigLayouts`) and encodes
everything else, so the known audio trigger should not reach it: the stands' PCE streams (ADTS
channel configuration 0) read `channel_layout=unknown` to ffprobe 8.1.2 in the transcoder's image
(`sha-2b6d29a`, checked 2026-09-29) and are encoded. What is left is a PCE that declares exactly a
configuration's elements, which ffprobe names after it and the transcoder copies ("What the name
cannot tell" there); whether Chrome refuses that one is not measured.

**The old route's unbounded recovery stays as it is.** `hls-manager.js` recovers every fatal media
error of a stream without a guard, with no counter. Bounding it would change what every browser that
declares no audio token gets — a stream that recovers from the fourth transient media error today would
stop — and there is no measurement of how often that happens, so it is left exactly as it was
(`hls-manager.test.js` pins the listeners of a stream without a guard to the ones recorded at
`1bea6ac8`).

**No Compatibility mode on the old route.** The item's promise — "Converts the video on our side.
Use it if the picture looks wrong." — is false there: that video is already converted. What it would
have to offer is "stereo sound", a different item with its own text in 11 languages; the failures a
guard sees now restart by themselves, and the ones it cannot see (a 5.1 track that plays with a
wrong layout, a silent centre channel) are the transcoder's to get right for every browser at once —
the owner's device matrix, not a per-viewer escape. Since the audio's stage 5 every browser
declares every audio token it answers and `?audio=off` is the only way back to stereo, by address; whether an
audio item in the menu is worth its text in 11 languages now is the owner's call (not built). On a passthrough the item stays what it was: the restart with no declaration.

Seen in a real browser (headless Chrome 154.0.8037.58 on a Mac, hls.js 1.6.14, 2026-09-29): the
declaration of a browser that never opened `?audio=` — Chrome answers `aac51` and no Dolby, so it
declares `…,hdr-pq,aac51`, the same with `?audio=on`, the video alone with `?audio=off`; the PCE
stands (the old route's master: given up after one recovery in 228–320 ms, `by: buffer`; the muxed
playlist: after hls.js's two recoveries in 26–126 ms; standard 5.1 plays 5.7–6.1 s of 6 s — the same
as before these changes); a passthrough-shaped fMP4 (HEVC, the audio a rendition of its own) with AAC
stereo plays, with an `ec-3` variant Chrome refuses the manifest (`codecs_rejected`, now the video's),
with an E-AC-3 init behind a master that says AAC the player dies (the known gap above); the MediaError
texts quoted under "Which side failed". Chrome decodes neither AC-3 nor E-AC-3, so everything about a
Dolby decoder at work is **not verified**: what Safari, iOS, Edge and Opera raise when one fails and
whether their messages name the stream (Safari's never do, by WebKit's source), AC-3 copied in Safari,
Edge on Windows at all, an output device going away under a stream (`AUDIO_RENDERER_ERROR`, from
Chromium's source only), the `CHANNELS`/`CODECS` the transcoder writes (the page reads what the spec
says it will, and falls back on hls.js's own parse), the Dolby path on iOS (hls.js on a
ManagedMediaSource since 2026-09-30, native HLS with `?mms=off`) — the owner's device matrix.

#### Watching the audio after the rollout

Since the audio's stage 5 every browser declares every audio token it answers (see "Who declares
audio"). What to look at, and against what. The browser is Umami's session; `audio` is the start's
class on `stream-start`, both fallbacks and `player-dead` / `player-revived`. Dolby is copied only
into a passthrough's fMP4, and a passthrough is a small share of video starts (the day to 2026-09-30:
10% on iOS, 15% in Chrome on Windows, 3% in Edge), so `dolby` rows exist only on that route while
`none` is mostly the old route's: a class is compared with another **only on the same route and
browser**. `stream-start` fires after 5 s of
playback, so a start that failed is not among the starts — its restart plays on the old route and is
counted there — and the failure share of a passthrough class is `hevc_fb / (pt_starts + hevc_fb)`.

Per browser, audio class and route — starts, the video's and the audio's fallbacks, and players that
never started (Umami's database, the umami pod's `DATABASE_URL`, read-only and with a timeout:
`PGOPTIONS='-c statement_timeout=60000 -c default_transaction_read_only=on'`; the CTEs keep it on the
`created_at` indexes: 1.5 s for a day on 2026-09-30):

```sql
WITH ev AS (
  SELECT event_id, session_id, event_name FROM website_event
  WHERE website_id = '76c80a8c-0ecd-418c-9e1c-09d5fff43271'
    AND created_at > now() - interval '1 day'
    AND event_name IN ('stream-start', 'hevc-fallback', 'audio-fallback', 'player-dead', 'player-revived')),
dat AS (
  SELECT website_event_id, data_key, string_value FROM event_data
  WHERE created_at > now() - interval '1 day' AND data_key IN ('audio', 'route', 'isVideo')),
r AS (
  SELECT ev.event_id, ev.event_name, s.browser, s.os,
    coalesce(max(d.string_value) FILTER (WHERE d.data_key = 'audio'), '?') AS audio,
    coalesce(max(d.string_value) FILTER (WHERE d.data_key = 'route'), '') AS route,
    max(d.string_value) FILTER (WHERE d.data_key = 'isVideo') AS isv
  FROM ev JOIN session s ON s.session_id = ev.session_id
  LEFT JOIN dat d ON d.website_event_id = ev.event_id
  GROUP BY 1, 2, 3, 4)
SELECT browser, os, audio,
  count(*) FILTER (WHERE event_name = 'stream-start' AND route = 'passthrough') AS pt_starts,
  count(*) FILTER (WHERE event_name = 'hevc-fallback') AS hevc_fb,
  count(*) FILTER (WHERE event_name = 'audio-fallback' AND route = 'passthrough') AS pt_audio_fb,
  count(*) FILTER (WHERE event_name = 'player-dead' AND route = 'passthrough')
    - count(*) FILTER (WHERE event_name = 'player-revived' AND route = 'passthrough') AS pt_dead,
  count(*) FILTER (WHERE event_name = 'stream-start' AND isv = 'true' AND route <> 'passthrough') AS other_starts,
  count(*) FILTER (WHERE event_name = 'audio-fallback' AND route <> 'passthrough') AS other_audio_fb,
  count(*) FILTER (WHERE event_name = 'player-dead' AND route <> 'passthrough')
    - count(*) FILTER (WHERE event_name = 'player-revived' AND route <> 'passthrough') AS other_dead
FROM r GROUP BY 1, 2, 3 ORDER BY pt_starts DESC, other_starts DESC;
```

`?` is an event from a page older than the field. `hevc-fallback` carries no `route` — it is always a
passthrough given up. `stream-start` counts only video players (`isVideo`); `player-dead` has no
`isVideo`, so `other_dead` includes `<audio>` players (their class is always `none`). `pt_dead` /
`other_dead` are deaths less revivals. The baseline before any audio token went out, the day to
2026-09-30 00:30Z (every class `?`): `ios` 49 passthrough starts / 15 `hevc-fallback` / 430 old-route
starts, `crios` 16 / 6 / 90, Safari on a Mac 18 / 4 / 65, Chrome on Windows 92 / 3 / 514,
`edge-chromium` 3 / 1 / 86, `opera` on Windows 2 / 0 / 36; `player-dead` on the old route: `ios` 6,
`crios` 1. What each column says:

- **`pt_audio_fb` with `by = 'unpinned'`, and `hevc_fb`, of `dolby` against `none`, per browser, over
  their own `pt_starts`** — a Dolby decoder that fails with nothing to blame it is charged to `dolby`
  first ("Dolby first, without an HEVC strike"), so it shows up in `pt_audio_fb` as `unpinned`; a real
  HEVC failure on a Dolby file shows up there too and then, after its restart, in `hevc_fb`. The
  rollback rule: a browser whose `dolby` fallbacks (both) are more than twice its `none` share. iOS (`ios` + `crios`) has about 65 passthrough starts a
  day and Safari on a Mac about 20; with about 44% of HEVC sources carrying Dolby, a doubling reads
  within days on iOS and within a week or two on a Mac.
- **`pt_audio_fb` with `by`** (the next query) — `codec` is the browser refusing the codec it declared,
  `buffer` / `message` a decoder or parser that failed and said so. Edge and Opera, the untested
  browsers, have 3 and 2 passthrough starts a day: about one Dolby start a day each, so a conclusion
  about them takes **weeks**, not days.
- **`pt_dead` of `dolby` against `none`** — a start that died with no error and no fallback (the way
  native HLS on iOS refused a PQ variant): the only event of such a death. It does **not** see an
  element error before metadata in Chrome (the element pauses and the watch disarms — "Known gap"
  above), so a missing `pt_dead` is not proof of none.
- **`other_audio_fb`, `other_dead` of `aac51` against `none`** — AAC 5.1 on the old route, where both
  classes live.

The audio's fallbacks in detail — what was charged, what blamed it, where:

```sql
WITH ev AS (
  SELECT event_id, session_id FROM website_event
  WHERE website_id = '76c80a8c-0ecd-418c-9e1c-09d5fff43271'
    AND created_at > now() - interval '7 days' AND event_name = 'audio-fallback'),
dat AS (
  SELECT website_event_id, data_key, string_value FROM event_data
  WHERE created_at > now() - interval '7 days'
    AND data_key IN ('cls', 'audio', 'by', 'reason', 'route', 'path'))
SELECT s.browser, s.os,
  max(d.string_value) FILTER (WHERE d.data_key = 'cls') AS cls,
  max(d.string_value) FILTER (WHERE d.data_key = 'by') AS by,
  max(d.string_value) FILTER (WHERE d.data_key = 'reason') AS reason,
  max(d.string_value) FILTER (WHERE d.data_key = 'route') AS route,
  max(d.string_value) FILTER (WHERE d.data_key = 'path') AS path
FROM ev JOIN session s ON s.session_id = ev.session_id
LEFT JOIN dat d ON d.website_event_id = ev.event_id
GROUP BY s.browser, s.os, ev.event_id ORDER BY 1, 2;
```

Who answers which audio token (`codec-support`, once a week per browser; its booleans are stored as
`'true'`/`'false'`; events before the multichannel-audio release, 2026-09-28 21:10Z, carry none of
the three, so start a window there or later) — the numbers under "Who declares audio" come from this
with that window:

```sql
WITH e AS (
  SELECT event_id, session_id FROM website_event
  WHERE website_id = '76c80a8c-0ecd-418c-9e1c-09d5fff43271'
    AND created_at > now() - interval '7 days' AND event_name = 'codec-support'),
d AS (
  SELECT website_event_id, data_key, string_value FROM event_data
  WHERE created_at > now() - interval '7 days' AND data_key IN ('ec3', 'ac3', 'aac51'))
SELECT s.browser, s.os, count(DISTINCT e.session_id) AS sessions,
  count(DISTINCT e.session_id) FILTER (WHERE d.data_key = 'ec3' AND d.string_value = 'true') AS ec3,
  count(DISTINCT e.session_id) FILTER (WHERE d.data_key = 'ac3' AND d.string_value = 'true') AS ac3,
  count(DISTINCT e.session_id) FILTER (WHERE d.data_key = 'aac51' AND d.string_value = 'true') AS aac51
FROM e JOIN session s ON s.session_id = e.session_id
LEFT JOIN d ON d.website_event_id = e.event_id
GROUP BY 1, 2 ORDER BY 3 DESC;
```

The server's count (Prometheus, every browser together, by the class the restart carried):
`sum by (class, reason) (increase(webui_passthrough_fallback_total{class=~"dolby|aac51"}[1d]))` —
against the video's `class=~"hevc.*"`. A strike takes a class out for 7 days, so a rising `dolby`
count followed by a fall is browsers striking it out, not a fix.

**Rollback.** Dolby alone: `mayDeclareDolby` → `=== 'on'` (`decode-declaration.js`; its tests). All
audio: `mayDeclareAac51` the same. For one viewer: `?audio=off`. A rollback reaches a viewer with the
next full page load and lifts no strike already written (`wt-decode-fallback`, 7 days). A player that dies with no error and no fallback is `player-dead`'s,
counted per class since it carries `audio` (2026-09-30); one whose element errs before metadata and
pauses is not counted at all yet (see "Known gap").

#### Compatibility mode

The "More" menu (`SettingsControl.jsx`) shows **Compatibility mode** (`player.compatMode`, hint
`player.compatModeHint`) only on a passthrough stream — the menu itself appears for it even without a
next file. It is the fallback with reason `user`: the viewer who sees a wrong picture no check catches
(Dolby Vision without its metadata, HDR on an SDR screen, green frames) restarts the file converted on
our side, and the file is remembered; no class is struck — it is the viewer's judgement, not a decoder
failure.

### `playback-quality`

The declaration counts software decoding, so its risk — a slow machine dropping frames of a 4K HEVC
film without any decoder error — is measured. One event per page load, when the first `<video>` on it
has played **60 s of media** (the sum of the forward `timeupdate` steps of at most 2 s: seeks, skipped
gaps and steps back do not count); never for audio. Not tied to `codec-support`'s weekly sample: this
is about a play, not a browser. A separate event because `codec-support` goes out at the first frame
and counts viewers; holding it a minute would drop everyone who stops sooner. Cost: one listener doing
arithmetic on `timeupdate`, one `getVideoPlaybackQuality()` at the mark, the send in an idle callback;
nothing touches the element. No API, or no `window.umami` at the mark: nothing is sent and the page is
not marked (`window.__wtPlaybackQuality`).

| key | what |
|---|---|
| `dropped`, `total` | `droppedVideoFrames` / `totalVideoFrames` since the element's last load |
| `drop_pct` | `dropped / total` in percent, 2 decimals; absent when `total` is 0 (no frames after a minute is a reading too) |
| `played` | seconds counted, rounded (≈60) |
| `height` | `videoHeight` (0 unknown) |
| `rate` | `playbackRate` |
| `hidden` | some counted step happened in a hidden tab: a background tab may stop rendering, read those apart |
| `decode` | the declaration at that moment, as in `codec-support` — audio tokens included since the multichannel-audio release (see "Reading `decode`") |
| `src`, `tc`, `route`, `pl`, `emb` | the same stream facts as `codec-support`, read at the mark; `route` is the session's (`data-video-route`, `''` without one) — once HEVC is passed through, `tc` is true for a passthrough and a re-encode alike, and the drops of a software decoder are the passthrough's |

For example: `{dropped: 30, total: 1500, drop_pct: 2, played: 60, height: 2160, rate: 1, hidden: false,
decode: 'hevc8,hevc10', src: 'hevc', tc: false, pl: 'hlsjs', emb: false}`. Today the transcoder
passes no HEVC through, so this is the baseline: re-encoded H.264 where `tc` is true, while
`src: 'hevc'` with `tc: false` — HEVC in MP4, repackaged as it is without a transcoder session — is
where software HEVC decoding already happens (plan §6: ~35% of HEVC starts).

### `discover-release-check`

Discover hides HEVC and HDR releases by what their names say (docs/discover.md, "Video switches"),
and how often those names are wrong on its lists is not measured. A click on a release there leaves
what the name said in sessionStorage (`wt-discover-release`, 30 min); on the first frame of that
release (`data-resource-id` = the recorded infohash) the player takes the record and sends
`rel_codec`, `rel_hdr`, `rel_dv5`, `rel_uhd` against `src` (the media probe's codec, as in
`codec-support`) and the session's `route` / `reason`. Once per record; a record for another release
is left for its own page. `lib/discover/release-check.js`; every storage access is caught, since the
read can run inside the codec effect (an element already playing at mount) and Preact drops a
component's remaining effects after one throws.

## Next episode / next track — `next-item.js`, `next-item-go.js`

Design, numbers and the owner's decisions: `docs/superpowers/specs/2026-09-20-next-episode-design.md`.
86% of viewers who finish an episode open the next one by hand; the feature is about the minute that
transition costs.

**Server.** The stream job resolves what follows the file it renders (`jobs/scripts/next_item.go`
over the pure `services/next_item`): the next `(season, episode)` of the series among this torrent's
files — specials never bridge to regular seasons, a two-episode file is followed by what comes after
its last episode, an episode without a file is skipped — or, for audio, the next audio file of the
directory in natural order. A video that is not an episode has no next. It lands on the player
element as `data-next-item-id|path|kind|label`; **absent = the feature is off** for this stream
(a film, the last file, an embed, a failed lookup — and the kill switch). Bounded at 4 s.

**Track choices travel as intent** (`models.TrackCarry`, `handlers/action/carry.go`). Track ids mean
nothing in another file, so `readCarry()` reads the picker's current chips into `carry-*` form
fields — audio language + label; subtitles off, or language + origin — and the picker resolves them
against the new file's lists to an item id, which goes down the saved-choice path (the one place that
knows about locked items and the "None" switch). A carry outranks the file's own saved choice and
the ladder; same language from another origin beats the ladder; a carry the file cannot honour
changes nothing. It is part of the job cache key. The subtitle delay does not travel: it belongs to
one subtitle file. **Subtitles travel only when they are the viewer's choice** — `data-saved` on the
playing chip, rendered by the server for a saved choice and moved by `markTrack` on every persisted
pick. What the ladder or the audio-switch rule turned on is decided again by the next file's own
ladder (review 2026-09-28: carried, the ladder's English track beside a translation offer arrived as a
saved choice and beat the next episode's own track in the viewer's language). Audio still carries
what plays.

**Client.**
- Button right after Play (`NextIcon`: a triangle with a bar on its right), key `n` / `Shift+N`.
- `advancePlan()`: prewarm at 90% but never more than 5 min early (a prepared render and its
  transcoder session live ~10 min), only while playing in a visible tab; the "up next" card in the
  last 10 s (earlier when the credits are known, see below), video only, always a 10 → 0 countdown.
- `atEnd()`: `go` / `offer` (autoplay off) / `ask` ("still watching?" after 3 automatic moves with no
  pointer or key event — a sleeper must not warm up and transcode a season) / `stay` (cancelled).
- Autoplay is a remembered setting (`player-prefs` `autoplayNext`, default on) behind a switch
  (the design system's `toggle toggle-soft`, the same as the subtitles switch — a home-made solid-pink
  one was tried and looked like neither). It lives in the **"more" menu** (three dots, always the last control on the right; `SettingsControl.jsx`), with
  its name next to it and reachable at any time, for video and audio alike, and on the card as well.
  A bare switch in the audio bar was tried first: it said nothing about what it switched; then a
  gear, which read as bold beside the outline icons and promised more than a menu of one switch. The
  button is as narrow as its glyph (a square one left the dots stranded in empty space) and shows
  only where there is a next file. Its menu and the speed menu share `useAnchoredPopover`
  (top-layer popover placed from the button's rect, outside press / Escape / scroll close it) and
  the `.wt-player-menu` styles.
- **Music has no card at all** (owner): tracks follow one another like an album, or do not, by the
  switch. No "still listening?" either — `atEnd()` for `kind: track` is `go` or `stay`.
- The move itself (`createNextItemGo`): the next file's render is fetched off the page
  (`background-render.js`, with a silent Turnstile token for anonymous viewers), the old player is
  destroyed **keeping its stage**, the new one is mounted into the same stage, the address and title
  are updated, and the page around (`#content`: file card, list) is synced — at once when windowed,
  on leaving fullscreen otherwise. `syncPage` fetches the fresh `#content` aside, moves the live
  player into its new log container, then swaps. A carried AI translation is kicked with one HEAD
  to the track so its first lines are ready. What cannot be done quietly (an error card, a cap
  modal, a Turnstile checkbox) falls back to opening the next file the visible way.
- A next file the viewer had already started asks "continue from … / start over" like any other
  file. The first version answered it silently (the settings-restart note, `markAutoResume`); that
  read as the saved position being ignored, and the question is the viewer's to answer.
  `resumeAt()` remains for the settings restart: a file ≥90% finished restarts from the top.
- While the next file loads (not prewarmed: a stream start like any other, up to a minute) the
  player says so: the buffering label, a spinner in the Next button, the card's kicker reads "Loading
  the next one…". Between the two players the stage keeps its height
  (`.wt-player-stage--switching`) — an empty block has none, and the page jumped.
- A player mounted by a move is **loading, not paused** (`awaitStart`): until its first frame it
  shows the buffering label, not the big Play button — both at once was two answers. Ends with `playing`,
  with the resume prompt (a question only the viewer can answer), or after 10 s (autoplay refused:
  Play is what they need). The empty stage shows the player's own spinner (`--empty`, the same SVG
  as `LoadingSpinner`), removed as soon as a player is in it.
- The file being left is **paused** the moment the move starts (button, key, card, the countdown): it
  used to play on under the spinner for as long as the next one took to start, and its saved position
  moved with it.
- **The wait is narrated.** `fetchStreamRender({ onProgress })` reports the job's log as it
  happens — the running step, and its status under it ("warming up torrent client, downloading
  10 MB — 37%") — and the card shows the latest line while the viewer waits. Kept from the silent
  prewarm too, so pressing Next midway shows where it is.
- **A cold start gets minutes** (`NEXT_RENDER_TIMEOUT_MS`, 10 min), not the 30 s a settings restart
  allows: with 30 s every slow start timed out into the visible fallback — a full page load — and
  the viewer waited half a minute and then watched the page restart. The job's own deadlines decide
  when a start has failed. The fallback remains for what cannot be quiet: an error card, a cap
  modal, a Turnstile checkbox.
- **One sync, the latest.** Several moves can happen in one fullscreen sitting; each used to leave
  its own "sync when fullscreen ends" behind, and on exit they raced — the slowest won, which could
  put episode 2's card and start form under episode 3's picture. `scheduleSync` keeps one pending
  URL and one listener, and a generation lets a newer `syncPage` overtake an older one in flight.
- **The live subtree is the whole action view** (`liveRoot`: the direct child of the log container),
  not `stage.parentNode`. The stream template wraps the video in a `<div class="relative">`, so the
  parent was an inner element and the view's marker (`data-async-view`, whose destroy handler is
  `destroyPlayer()`) sat on its ancestor: the sync told it that it was going, and the player that had
  just mounted was destroyed — "the new episode appears, then only the file list". For the same
  reason `mountOnStage` adopts the *content* of the render's wrapper rather than the wrapper, which
  nested one level deeper on every move.
- **`syncPage` runs the view lifecycle** around the live player (`destroyViews` / `activateViews`,
  split out of `lib/loadAsyncView.js` with a `skip` subtree): without it the new file list came back
  with its scripts never run (`resource/select.js`: no multi-select, no archive).
- **The carried choice is saved** once the new player is up (`persistDefaults` → the same PUT a chip
  click makes): a default is not a saved choice, and the next plain start of that file — a settings
  restart, a reload — would have asked the ladder again and could flip what the viewer carried over.
- A failed mount falls back to the visible way in: the old player is already gone by then, and a
  half-built page is the one outcome worse than a reload. A page without the start form or
  `#content` has no "next" at all (`canMoveOn`).
- **The prewarm does not ask whether the tab is visible**, and follows the element's `timeupdate`
  rather than `state.currentTime` (fed by `requestAnimationFrame`, which a background tab does not
  run at all). Music lives in a background tab: the first night in production 11 of 14 automatic
  moves between tracks came unprepared, ~8 s of silence between songs. And a film left to play out
  in a background tab moves on at `ended` all the same — refusing to prepare the next file does not
  save its start, it only moves it to the moment it hurts. The card stays on the state-driven
  effect: it is something to look at.
- **Music prewarms from the middle** (`PREWARM_AT_TRACK` 0.5): 10% of a three-minute song is
  eighteen seconds, less than a cold start, and the album would stutter between tracks.
- **The card is a top-layer popover** docked to the player's bottom-right corner, above the control
  bar (`useDockedPopover`): inside the frame (`overflow: hidden`) a phone-width player cut its top
  off. It is re-placed on scroll / resize / a change of its height, and re-shown on a fullscreen
  change (the top layer is ordered by arrival).
- Look: the player's own vocabulary (the glass buttons of the resume prompt), not the site's —
  a pink button here means a homepage CTA. `NextIcon` is Play's exact triangle plus a bar, in a
  30×24 box; the button is wider by what the bar adds, so the two triangles match.

**Credits from the container's chapters — `jobs/scripts/credits.go`.** The file's own answer, and the
first one asked: `content-prober` runs ffprobe with `-show_chapters` (since 2026-09-21), the stream
job reads the earliest chapter whose title names the closing ("End Credits", "Ending", "ED", "Outro",
«Титры», …) inside the 25 s … 10 min window before the end, and puts it on the player as
`data-credits-at`. Earliest, because "Ending" is followed by "Preview" and "End Credits" by
"Post-credits scene": the decision point is where the first begins, and there is a countdown and a
Cancel from there. Generic names ("Chapter 12") say where, not what, and match nothing. Known from
the first second, needs no subtitles (the subtitle guess found something in 11% of lookups the first
night: 78% of streams had no whole-file track). Probes cached before the change have no chapters;
they refresh within a week. RE2's `\b` is ASCII-only — the Cyrillic alternatives go without it.

**Credits from subtitle timings — `credits.js`** (when the chapters say nothing)**.** Dialogue ends, credits begin: the end of the last
cue + 3 s. It only moves things *earlier* — the card from "the last 10 s" to "when the talking
stops", and the prewarm a minute ahead of that (never earlier than a prepared render lives, 8 min).
With autoplay on, the card counts down `COUNTDOWN_S` (10 s) from the start of the credits and then
moves on, with Cancel on it (owner's decision, 2026-09-20; the first version only showed the card
early and moved on `ended`). `countdown()` runs in **film time** — a pause pauses it, a seek back
withdraws it, there is no timer to cancel. Without known credits the card comes up ten seconds
before the end, so the number is the same 10 → 0 (the first version came up 25 s out and printed
"next in 20 s"). The ten seconds start when the **card** does (`shownAt`), not at the credits: a viewer
who seeks into the credits arrived after "credits + 10 s" and was told "next in 0 s". The price of
a wrong guess is ten seconds to press Cancel; the guards below and the
discarding of late guesses (a post-credits scene) are what keep that rare. Timings do not depend on language, so any whole-file track
does: cues of an already loaded `<track>` first (authored times, `__absStart`), otherwise **one**
request for a whole-file chip's VTT (never a translation — that starts a paid job; never a muxed
track — hls.js feeds those segment by segment). Looked for once per file, past 60%. Guards:
≥20 cues; a trailing run of ≤2 cues / ≤15 s after ≥60 s of silence is a translator's signature and
is dropped (a real post-credits scene is more lines and survives, which lands the guess at the end
and discards it); a result later than 25 s before the end or earlier than 10 min before it is no
result. Event `next-item-credits {source: loaded|fetched|none, found, lead_s}`. Container chapters
("End Credits") would be more exact; `content-prober` does not ask ffprobe for them yet.

Between two players the new container takes the old picture's `aspect-ratio`
(`initPlayer({ aspectRatio })`): until `canplay` reports the real one it had a default height, and
the controls, pinned to its bottom, jumped inside the held stage.

**The stage.** Fullscreen is requested on `.wt-player-stage`, a wrapper that outlives the player it
holds (`initPlayer(target, { stage })`, `destroyPlayer({ keepStage })`): a fullscreen element stays
fullscreen while it stays in the document, whatever happens to its children. On the player's own
container, as before, every transition would have ended fullscreen — and a browser does not re-enter
it without a gesture. iOS native video fullscreen cannot be kept; accepted.

Events: `next-item-shown`, `next-item-prepared {ok}`, `next-item-go {how: auto|button|key|card,
prewarmed, fallback, fullscreen, wait_ms}`, `next-item-cancel`, `next-item-autoplay {on}`,
`next-item-still-watching`. `wait_ms` is the number the feature exists for; the baseline is ~58 s.

## "Watched" and the credits — `models.IsWatched`

One rule, two ways to satisfy it, whichever comes first: 90% of the duration (as always), or the start
of the credits when the player could tell where that is (`credits.js`; sent as `credits_at` with
every `PUT /watch/position`, `useWatchHistory`). An episode with ten minutes of credits ends, for the
viewer, at 80% — by the 90% rule alone it stayed unfinished forever: in Continue watching, in the
series' progress, and as the file offered for resume. It is the same moment the player offers the
next episode. `credits_at` is client input, so it is believed only inside the window credits can
occupy (25 s … 10 min before the end, the bounds `credits.js` uses); outside it the 90% rule stands
alone. The lookup therefore runs for signed-in viewers of any video, not only when there is a next
file. `watched` is still recomputed on every update (rewinding un-watches, as before).

## Saved position — `useWatchHistory`

A position under `MIN_SAVED_POSITION` (30 s) is neither saved nor offered: it is a viewer who looked
in and left, and "Continue from 0:12?" is a question about nothing. Exactly 0 still goes through —
that is "Start over" resetting a real position. A file the server calls `watched` (90%, or past the
credits — `models.IsWatched`) is not offered for resume either.

## Seeking inside a transcoder run — `local-seek.js`

A session plays a **run**: FFmpeg started at `seekOffset` (film time) and writes segments as fast as
the torrent feeds it — no `-re`, no list size — so the playlist holds everything from the start of
the run to wherever FFmpeg has got to. Every point in between is a plain `video.currentTime =`.
Until 2026-09-20 *every* seek in a session was a POST to the transcoder: a new FFmpeg, a frozen
frame, a second or more of nothing — including the ten seconds of a double tap and the fifteen of an
arrow key, which almost always land inside what is already there.

`handleSeek` asks `localSeekTarget(filmTime, seekOffset, producedEnd)` first. `producedEnd` is the
playlist's `totalduration` (hls.js) or the element's `seekable` end (native HLS). Local when
`0 ≤ filmTime − seekOffset ≤ producedEnd − EDGE_S` (8 s = two segments: the edge of a growing
playlist is no place to aim for); otherwise the session seek as before — back before the run began,
or forward past what has been produced. The offset does not change on a local seek, so cue shifts
and the translation's timeline stay put; the rest is what a direct seek does (`onDirectSeek`).

Event `player-seek {local, session}` — counts, sent once the seeking stops (a held arrow key is
thirty seeks a second).
