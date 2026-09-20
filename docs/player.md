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
they have no other face.

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
  last 25 s, video only. No countdown — the move happens on `ended`.
- `atEnd()`: `go` / `offer` (autoplay off) / `ask` ("still watching?" after 3 automatic moves with no
  pointer or key event — a sleeper must not warm up and transcode a season) / `stay` (cancelled).
- Autoplay is a remembered setting (`player-prefs` `autoplayNext`, default on), toggled on the card.
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
- Look: the player's own vocabulary (the glass buttons of the resume prompt), not the site's —
  a pink button here means a homepage CTA. `NextIcon` is Play's exact triangle plus a bar, in a
  30×24 box; the button is wider by what the bar adds, so the two triangles match.

**The stage.** Fullscreen is requested on `.wt-player-stage`, a wrapper that outlives the player it
holds (`initPlayer(target, { stage })`, `destroyPlayer({ keepStage })`): a fullscreen element stays
fullscreen while it stays in the document, whatever happens to its children. On the player's own
container, as before, every transition would have ended fullscreen — and a browser does not re-enter
it without a gesture. iOS native video fullscreen cannot be kept; accepted.

Events: `next-item-shown`, `next-item-prepared {ok}`, `next-item-go {how: auto|button|key|card,
prewarmed, fallback, fullscreen, wait_ms}`, `next-item-cancel`, `next-item-autoplay {on}`,
`next-item-still-watching`. `wait_ms` is the number the feature exists for; the baseline is ~58 s.
