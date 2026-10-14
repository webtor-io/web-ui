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

- **Marked** at the press. For an anonymous visitor by `lib/turnstileAction.js`, before
  its check (about a second, up to 15 s silent, two minutes when Cloudflare wants a
  click: until 2026-10-14 the button looked idle through all of it); otherwise by
  `lib/async.js`, where the request begins. A second submit of a busy form is swallowed
  in both places -- in Turnstile's before the check, which would spend a token and
  write its step over the running job's log. Turnstile's own re-submit with the token
  carries `data-turnstile-ready` and is let through. A press dropped because another
  form's check is running closes the dialog its onclick opened (TAR/ZIP).
- **Released** by the job log (`lib/progressLog.js`, first thing in `renderMessage`) on
  `error`, `download`, `redirect`, and `close` of a job that rendered nothing; a job that
  rendered a player is released by `app/action.js` when the player is shown
  (`player_ready`, or `player_show` for a card before the first frame), or 30 s after
  the template as a backstop. Until 2026-10-14 `rendertemplate` released it: the player
  was still buffering out of sight, the button was live, and a press replayed the job
  over it. Also released by a failed request
  or a non-OK answer (`async.js`: a 400 -- Turnstile refused, bad arguments, the job did
  not start -- renders `action/post`'s card with no job log, so nothing else would ever
  release it; until 2026-10-14 the button stayed dead for the full backstop under a card
  that said "try again"); by `BUSY_MAX_MS` (15 min, a backstop for a lost SSE).
- **One target, one working button.** Watch and Download of a file card share
  `#log-<item>`; a new job's response replaces the log, so `markBusy` releases any other
  busy form with the same target — nothing else could any more.
- **The register is the DOM**, not a module-level Map: `async.js` and `progressLog.js`
  live in different bundles and each gets its own copy of the module (see the
  "Общее состояние JS" row in `CLAUDE.md`).

Not done on purpose: restoring the busy state across a page reload.

### What the wait leaves behind — measured, 2026-10-14

Umami, 7-day windows before (09-12..18) and after (10-06..12) the busy button and the
sticky bar: a repeat Watch press on the same page within 120 s fell from 43.2% to 31.8%
of streaming sessions (mobile 49.1 → 38.0%), Download from 34.4% to 27.1%, and has not
moved since the first week. The feedback still renders below the buttons, under the fold
on 768–844 px tall screens, and nothing brings it into view.

Two measurements were added to follow it:

- `busy-press` (Umami, `countBusyPress` in `lib/actionBusy.js`, bound by `lib/async.js`
  to every action form): the first press on a working button in each busy spell, with
  `action` and `wait` (seconds busy so far). A busy button has no pointer events, so the
  press lands on its form and no `data-umami-event` sees it. The listener sits on the
  form, not on document: iOS Safari sends no click for a tap on an element without a
  click listener of its own (or an ancestor's below `<body>`) or `cursor: pointer`.
- `download-start` (Umami, the "start download" link of `action/download_file`, props
  `kind` file|dir, `cached`, `tier`) over `webui_download_ready_total{kind,cached}`
  (Prometheus, counted where the job emits that card): how many finished downloads the
  viewer actually starts. The card needs a second click and used to render off-screen.
  Read the ratio as a trend per `kind` and `cached`, before and after a change, not as a
  conversion rate: a replayed card (the same visitor pressing Download again inside the
  job id's 10-minute bucket) sends another click and no counter, every click counts,
  copying wget/curl/URL sends nothing, and blocked trackers are missing from the
  numerator. Deduplicate the clicks on (session, url, kind, 10-minute bucket) and divide
  by `sum by (kind, cached) (increase(webui_download_ready_total[1d]))`.

## Sticky torrent status — `lib/stickyStatus.js`

A fixed bar under the navbar (`resource/status_sticky` in `views/resource/get.html`,
rendered at the top of `main`, outside the cards — inside one it sat under
`overflow-hidden`). It carries the very same transfer status block as the card
(docs/transfer_status.html; no compact variant, no torrent name): `resource/status.js`
clones the card's `.tx` into `[data-status-mirror-for]` once (`mirrorBlock`, ids
suffixed `-sticky`), writes every SSE message into both, and broadcasts a
`torrent-status` event `{resourceId, state, moving}`.

Shown only when **both** hold: the real `#torrent-status` has gone under the navbar
(IntersectionObserver with `rootMargin: -72px`), and something moves: `moving` is the
view's `sticky` — the chain is up (bytes from the swarm, bytes to the viewer, the plan's
cap binding, or the viewer waiting for data; the chain gives way to the badge only 10 s
after the last movement). The badge — nothing moves — has no sticky bar. The mirror keeps
its last picture through a 1-second `unknown`/`idle` gap, and stays up with it (up to
8 s; one hold, the status view's: the event's `moving` is the held view's, and
`stickyStatus.js` has none of its own since 2026-10-03 — two timers on one rule could
drift apart). The hold outlives the status token's renewal.

- "Gone upwards" is `bottom <= rootBounds.top`, **not** `top < 0`: the observer fires at
  the crossing, when the top is still ~+28 px, and never again — `top < 0` worked on a
  flick and did nothing on a slow scroll.
- The observed element is re-acquired on the global `async` event (the status view
  reloads itself when its token expires).
- It slides out (`SLIDE_MS`) before `hidden` lands; state is tracked in a variable, not
  read back from `bar.hidden`.
- Keyboard focus and anchors stop below it while it is up: its height joins the navbar's
  in `html` `scroll-padding-top` (measured on every message), and goes when it does.
- No role and not live: the block is updated every second (what matters is said once by
  the card's `[data-tx-announce]`, docs/transfer_status.html).
- The player has `isolation: isolate` (`player.css`) so its internal z-indexes (up to 41)
  do not compete with `z-navbar` (30) and `z-sticky` (20).
- Browser automation cannot verify it: a hidden tab runs neither IO nor rAF.

## Warm-up size

See `docs/warmup.md` → "Why the stream warm-up is 10MB".

## Picking a file reloads the Content section only

The file links in `partials/list.html` target `#content` (`views/resource/get.html`,
template `resource/content`: the file card `#file` + the browser `#list`). They used to
target `main`, which re-rendered the header, restarted the status SSE and threw the
sticky status away on every click, then scrolled to the top of the page.

- `#content` carries `data-async-update-title` (the title names the file on
  non-enriched resources) and `data-async-update-nav` (language links must point at the
  new URL). Nothing else above the section depends on the picked file.
- `get_ads` is a script block, not a slot; it sits above `#content` so a file click does
  not run it again.
- The view's JS (`resource/get.js`) is not re-run by this swap. It listens for the global
  `async` event and scrolls `#file` into view (`scroll-mt-36`: navbar + sticky bar) when
  the card is out of sight — it is above the list the viewer clicked in.
- The snippet travels as `X-Layout`, so it uses `has` guards
  (`services/web/has_guard_test.go`). A page without `#content` falls back to `main`.
- Still targeting `main` on purpose: directory links when no file is picked, and the
  watched toggles (they keep the header button in sync).
