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
nudges by the gap-controller; the fatal handler restarts loading after a network error, and after a
media error hls.js 1.6's `recoverMediaError()` restarts it at the playhead itself (1.5.6's did not).
Disarmed by `STALL_RESOLVED`, a pause, the end, `MANIFEST_LOADING` (a session seek's reload),
detaching and destroying.

Not covered: on HTTP/2 (the stream host negotiates h2) a re-request shares the connection, so a
hang of the browser↔edge connection itself is not cured by asking again — only hangs further
upstream are; how often requests hang in production is not measured.

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
| `stream-start` | + `rate`, `subtitleDelay`, `route`, `reason`, `decl` | what the stream started with (remembered settings make no change event); the transcoder session's route and reason and the declaration it was started with (`data-video-route`, `data-route-reason`, `data-decode`; `''` where the element has none) — the base the HEVC passthrough is measured against |
| `hevc-fallback` | `reason`, `cls`, `path: mse\|native` | a passthrough given up to the old route (see "Passthrough: errors and fallback") |
| `audio-fallback` | `reason`, `cls: dolby\|aac51`, `path: mse\|native`, `route` | a file restarted without the multichannel audio its declaration made, on any route (see "Multichannel audio and the fallback"); never counted in `hevc-fallback` |

Read them as shares of `stream-start` sessions; mobile share for `player-tap-seek`.

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
| `decode` | string: the tokens this browser answers, joined with `,` — the `decode=` value of a start that takes part in both parts of the declaration and has no failure in memory; `''` for none. **Since the multichannel-audio release (2026-09) it carries the audio tokens too, for every browser** (the event asks every viewer the audio questions, whether or not it opted into declaring audio, `?audio=on`): a browser that reported `hevc8,hevc10` reports `hevc8,hevc10,aac51` — see "Reading `decode`" below. What a start actually sent is `stream-start`'s `decl` |
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
for `stream-start`'s `decl` of a browser opted into audio (the declaration the start sent).

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
- **`hdr-pq`** is the one asynchronous token: `decodingInfo` as `media-source` on the MSE path, `file`
  on the native one, without `hdrMetadataType`; `supported` is enough. Missing API, a rejection or no
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
  +352 B, `discover.js` +392 B, the player chunk +1.69 KB, `resource/get.js` +103 B.
- **Who declares audio.** The audio tokens (`aac51`, `ac3`, `ec3`) have an opt-in of their own,
  independent of the video's: `?audio=on` on any page (localStorage `wt-audio`; where storage throws,
  the page's own state), `?audio=off` takes the browser out (`applyAudioUrlSwitch`,
  `takesPartAudio`: `=== 'on'`). They go out only from a page that takes part in the declaration at
  all **and** opted into audio. Any other page sends the video part alone, its probe asks nothing
  about audio (`declarationSupport(env, {audio: false})`: no `decodingInfo` and no `isTypeSupported`
  for audio, no `wt-decode-audio` written) and `whenDeclared` waits for the video part only — the
  deep link's and the embed's 300 ms are not spent on tokens that are not sent. A cached audio answer
  from an earlier opt-in is not sent either. Why: the video declaration's stage 5 made it every
  browser's default after the owner's device matrix for HEVC; the matrix for audio (Dolby and AAC 5.1
  on real devices) has not run. The audio's own stage 5 is a separate, later one-liner —
  `takesPartAudio`: `=== 'on'` → `!== 'off'`, with its tests. The codec-support event is measurement,
  not the declaration: it asks every viewer the audio questions whatever the opt-in. Downstream
  everything keys on the declaration a start actually sent (`data-decode`, `DecodeRequest.DeclaresAudio`):
  the job's `data-audio-class`, the player's audio guard and the audio classes of the passthrough guard
  — a start without an audio token has none of them.
- **What** (`declarationFor(win, {resourceId, itemId})`), all or nothing:
  1. not taking part → no field;
  2. this file failed passthrough in this browser (memory below) → no field — its audio tokens
     included: the restart is the old route, stereo as it always was;
  3. the probe's video part answered → its tokens minus those the memory took away, then — only
     for a browser opted into audio — the audio part's; none at all → no field (not an empty one);
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
  it, `usePlaybackContext`), `window.__wtDecode.audio` the audio part's (null where it was not asked),
  and `whenDeclared` (the deep link's and the embed's 300 ms) waits for both where the audio part was
  asked, for the video part alone elsewhere. A browser that decodes no HEVC declares its audio
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
only one of `audio` / `video` (browser text, not a standard — not verified which browsers name the
stream). A decoder that fails after its append names no buffer: hls.js's later `bufferAppendError`
is named after whichever buffer appended next, so it is not read.

**The rule** (`audioFallbackClass`), for every guarded failure but `no_frames` (no picture is the
video's) and `user`:

| audio | fault | charged to |
|---|---|---|
| `dolby` or `aac51` | `audio` | its class |
| `dolby` | nobody | `dolby` — Dolby as it is is the part of such a session no browser was handed before, and giving it up keeps the video's route; where the video was at fault the restart fails again without Dolby and that failure goes the video's way (one extra restart, one wrong `dolby` strike) |
| `aac51` | nobody | the video's, as before: AAC 5.1 is what every browser that declares `aac51` without `ec3` gets for every multichannel source, so blaming it first would put a second restart and a wrong strike in front of most HEVC failures stage 4 is meant to see |
| any | `video` | the video's |
| none | any | the video's |

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
guard (`declaresAudio(data-decode)`), so a browser that has not opted into audio (`?audio=on`)
runs exactly the old handling. Where the audio is changed and no one pins the failure on the video, a fatal media error
from hls.js is recovered once and the next gives the file up to a restart without audio tokens,
charged to `aac51` (on the old route that is the only class that can happen: Dolby is copied only
into fMP4). Native HLS: the element's MediaError 3 → `decode_error`, 4 → `src_unsupported`, at once.
The measured case is AAC whose layout is in a PCE, copied for `aac51`: Chrome refuses the audio append
(MediaError 4, `CHUNK_DEMUXER_ERROR_APPEND_FAILED`, hls.js `bufferAppendError`) and every recovery
fails the same way — before this the old route called `recoverMediaError` for it without end, and
every start declared `aac51` again. Everything else on such a stream is the old route's: a failure
pinned on the video or audio the declaration did not change goes to `hls-manager.js`, which recovers
every fatal media error as it always has; the element's errors on the hls.js path stay hls.js's to
learn of at its next append.

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
the owner's device matrix, not a per-viewer escape. Until the audio's own stage 5 only browsers
opted in with `?audio=on` declare audio, and `?audio=off` takes one out. Whether an audio item is
worth its text at that stage is the owner's call. On a passthrough the item stays what it was: the restart with no declaration.

Not verified in a real browser: which errors each browser raises when a Dolby or 5.1 AAC decoder
fails, whether their MediaError messages name the stream, the `CHANNELS`/`CODECS` the transcoder will
write (the page reads what the spec says it will, and falls back on hls.js's own parse), the Dolby
path on iOS (native HLS) — all for the owner's device matrix.

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
one subtitle file.

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
