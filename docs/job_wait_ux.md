# The wait between a click and the result

Why it exists (Umami + Loki, 2026-09-19): from the first Watch click to the first frame
the median is **58 s**, p90 508 s, and only 4.8% of viewers are playing within 10 s. 59% of
streaming sessions pressed Watch more than once; two thirds of the clicks that recorded
nothing were the same button again, a median 17 s later. The wait is the swarm, so the
page has to say "working" where the eye is.

## Busy job-start buttons — `lib/actionBusy.js`

The five job starts (`/download-file`, `/download-dir`, `/preview-image`, `/stream-audio`,
`/stream-video`). While the job runs the form carries `data-busy="true"`; its button keeps
its label, swaps its icon for a spinner and gets `.btn-busy` (dimmed, `pointer-events:
none`, `aria-busy`) — **not** `disabled`: it is working, not refusing. The job log stays
where it is, under the buttons.

- **Marked** in `lib/async.js`, where the request actually begins — not on `submit`:
  Turnstile intercepts the first submit and re-submits the form itself
  (`docs/turnstile_actions.md`). A second submit of a busy form is swallowed there.
- **Released** by the job log (`lib/progressLog.js`, first thing in `renderMessage`) on
  `close`, `error`, `rendertemplate`, `download`, `redirect`; by a failed request
  (`async.js`); by `BUSY_MAX_MS` (15 min, a backstop for a lost SSE).
- **One target, one working button.** Watch and Download of a file card share
  `#log-<item>`; a new job's response replaces the log, so `markBusy` releases any other
  busy form with the same target — nothing else could any more.
- **The register is the DOM**, not a module-level Map: `async.js` and `progressLog.js`
  live in different bundles and each gets its own copy of the module (see the
  "Общее состояние JS" row in `CLAUDE.md`).

Not done on purpose: restoring the busy state across a page reload.

## Sticky torrent status — `lib/stickyStatus.js`

A fixed bar under the navbar (`resource/status_sticky` in `views/resource/get.html`,
rendered at the top of `main`, outside the cards — inside one it sat under
`overflow-hidden`). It mirrors the header badge and piece bar: `resource/status.js`
paints every `[data-status-badge-for]` / `[data-piece-bar-for]` from the same SSE and
broadcasts a `torrent-status` event `{resourceId, state, moving}`.

Shown only when **both** hold: the real `#torrent-status` has gone under the navbar
(IntersectionObserver with `rootMargin: -72px`), and a transfer is moving (`caching`,
`vaulting`, `vault_waiting` — `cached`/`vaulted` are answers, not progress).

- "Gone upwards" is `bottom <= rootBounds.top`, **not** `top < 0`: the observer fires at
  the crossing, when the top is still ~+28 px, and never again — `top < 0` worked on a
  flick and did nothing on a slow scroll.
- The observed element is re-acquired on the global `async` event (the status view
  reloads itself when its token expires).
- It slides out (`SLIDE_MS`) before `hidden` lands; state is tracked in a variable, not
  read back from `bar.hidden`.
- `aria-live="off"`: the badge repaints every second.
- The player has `isolation: isolate` (`player.css`) so its internal z-indexes (up to 41)
  do not compete with `z-navbar` (30) and `z-sticky` (20).
- Browser automation cannot verify it: a hidden tab runs neither IO nor rAF.

## Warm-up size

See `docs/warmup.md` → "Why the stream warm-up is 10MB".
