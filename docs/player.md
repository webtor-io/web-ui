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
| `stream-start` | + `rate`, `subtitleDelay` | what the stream started with (remembered settings make no change event) |

Read them as shares of `stream-start` sessions; mobile share for `player-tap-seek`.

## Codec support — `codec-support.js`

One Umami event, `codec-support`, that answers a transcoder question: what share of the people
who watch could play HEVC or AV1 as it is, if content-transcoder passed it through (fMP4 HLS,
`-c:v copy`) instead of re-encoding it to H.264. About a quarter of fresh sources are HEVC 1080p+
or AV1; their re-encode is slower than realtime and most of the transcoder's CPU.

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
| `src` | the source's video codec: `h264` / `hevc` / `av1` / `other` / `unknown` (no probe on the page) |
| `tc` | a transcoder session serves this stream (`data-session-id`) |
| `pl` | how the player plays it: `hlsjs`, `native` (the element's own HLS — iOS always), `direct` |
| `emb` | inside the embed |

For example: `{mse: 'mse', hvc: true, hev: true, hvc10: true, hvc4k: true, av1: true,
av1_10: true, av1_4k: true, n_hls: false, n_hvc: true, n_av1: true, mc: true, mc_hvc: true, mc_hvc_sm: true,
mc_hvc_pe: true, mc_av1: true, mc_av1_sm: true, mc_av1_pe: false, src: 'hevc', tc: true,
pl: 'hlsjs', emb: false}` plus the usual `tier`, `is_authed`, `user_id`, `lang`, `is_referral` on
the site (`docs/analytics.md`).

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
  player says so: spinner overlay, a spinner in the Next button, the card's kicker reads "Loading
  the next one…". Between the two players the stage keeps its height
  (`.wt-player-stage--switching`) — an empty block has none, and the page jumped.
- A player mounted by a move is **loading, not paused** (`awaitStart`): until its first frame it
  shows the spinner, not the big Play button — both at once was two answers. Ends with `playing`,
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
