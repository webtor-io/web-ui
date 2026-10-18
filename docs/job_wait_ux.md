# The wait between a click and the result

Why it exists (Umami + Loki, 2026-09-19): from the first Watch click to the first frame
the median is **58 s**, p90 508 s, and only 4.8% of viewers are playing within 10 s. 59% of
streaming sessions pressed Watch more than once; two thirds of the clicks that recorded
nothing were the same button again, a median 17 s later. The wait is the swarm, so the
page has to say "working" where the eye is.

## Busy job-start buttons — `lib/actionBusy.js`

The six job starts (`/download-file`, `/download-dir`, `/preview-image`, `/stream-audio`,
`/stream-video`, `/file-info`). While the job runs the form carries `data-busy="true"`; its button keeps
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
- **A card's form is the page's job too.** The no-peers card's "try again" and the slow
  card's "continue at slow speed" live inside the log they target; the response
  replaces the log and them with it. `markBusy` marks the page's own start form for that
  log and endpoint with them, and `clearBusyFor` releases every form busy for the log.
  `app/action.js` releases only the forms that were busy for its log when its response
  came in, and only while still busy from that press -- a newer press into the same log
  (Download after Watch) is not this job's to end.
- **The Turnstile script may not be there yet** (async, deferred: a slow phone; the
  `#action=stream` deep link presses at load). The press shows its step and waits until
  `SCRIPT_WAIT_MS` after the page loaded before it fails closed -- on stage, 3 of 7
  throttled loads used to be refused 0.5-3.4 s in. A check whose step went with its log
  (another file picked) is ended at the next press instead of holding every press on the
  page until its two-minute deadline. On a phone narrower than about 350 px the live
  checkbox is the compact one; once ticked, the line goes back to "Checking…".
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

## Where the result appears — `lib/stickyStatus.js` reveal

Measured on prod 2026-10-14 (headless Chrome, 390x844 to 1920x1080): the buttons sit
at y=640-680 on the unscrolled page and everything the job produced -- the Turnstile
step and checkbox, the countdown and percent, the no-peers and slow cards, the download
card, the player -- rendered in `#log-<item>` after the age footnote, at 736-775, under
the fold on 768-844 px tall screens. Nothing moved the page toward it.

- `#log-<item>` sits right under the button row (`partials/file.html`), the footnote
  after it. It is closed explicitly: it was `<div … />`, which HTML does not
  self-close -- #file was left open, #list was parsed into it, and the Vault and rating
  modals into #content, so picking a file wiped them.
- `reveal(el)` scrolls as little as possible: nothing when `el` is wholly between what
  is fixed at the top (`stickyBottom`) and the window's bottom; otherwise its bottom to
  the fold, but its top never under the bars (a player taller than the space shows from
  its top). Nothing inside a `<dialog>`, and nothing once the viewer scrolled themselves
  (wheel, touch, scroll keys; not Space on a button, which presses it -- Space on a link
  scrolls, and counts) after the press --
  `markBusy` notes the press (`notePress`), except for the player's own restarts
  (`resubmit`); order is kept as a sequence on `window.__reveal`, one record for every
  bundle. Not seen: the scrollbar dragged, Tab, a wheel over the Turnstile iframe; and
  one press counts for every job on the page. Smooth, or `instant` under
  `prefers-reduced-motion` (`auto` would follow `html.scroll-smooth`).
- Called by `app/action.js` on each new step, card, verdict (`inprogress`, `custom`,
  `error`, `warn`, a `done` with a message -- never `statusupdate`), on the shown player,
  and on a refused start's card, and on an image preview once it loads; by
  `lib/turnstileAction.js` on its step and on the live checkbox itself (its slot keeps
  65 px while the iframe sizes itself; revealing the whole block pushed it off-screen in
  a card's tall log); by `lib/toolIntent.js` on the file's buttons at once. The download
  card's "start download" takes focus (`preventScroll`), only while the viewer is still
  at the press and not typing elsewhere -- their Enter would start the download.
- The buttons on a phone (owner, 2026-10-14: variant D on the design canvas): Watch and
  Download share the width (`max-sm:flex-1` on their forms), the watched toggle is a
  44 px icon at the end of the row (label `max-sm:sr-only`, `aria-pressed`, neutral
  instead of pink), a file with only a download has it full width, and the size moves
  from the Download label to a line under the file name. 40 px tall, as before. File
  info (docs/file_info.md) is a 44 px outlined icon at the row's end (owner,
  2026-10-18: one line). With it the row is `data-crowded`: Download drops its icon
  (Watch keeps its own, the only sign of play/pause on a phone), both narrow their padding
  to 8 px and share the width by their labels (`flex-auto`), and a label too long for what
  is left ends in an ellipsis -- measured in 11 locales, at 360 px only beside the watched
  toggle in de/fr/es/cs/nl. From `sm` up the row is what it was, File info last and
  outlined.
- Watch / Listen pressed again while that file's player is on the page plays or pauses it
  instead of starting the stream anew (owner, 2026-10-18; `lib/streamToggle.js`). Its
  icon says what a press will do: pause while the player plays, play otherwise; the label
  stays (owner, 2026-10-18: a changing label read oddly and moved the row). The player
  carries `data-playing` (set on `play`, cleared on `pause`/`ended`/`emptied`/`error`) and
  the file card (`group/card`) swaps the icons with `:has()`, so a card rendered anew
  around a kept log reads the state from the player. Only a press toggles: restarts by
  code (Turnstile's pass, a preferred-language or passthrough restart) go through
  `requestSubmit()`, which clicks no button, and a busy form, a player without a source or a
  failed one mean a new stream. An audio file's button says Listen (`resource.listen`).

## The log in words — `lib/progressLog.js` with a `.job-stage`

The action log (`views/action/post.html`) used to be a monospace console in the job's own
jargon ("retrieving stream url", "probing content media info") with the only honest
progress -- the countdown to the no-peers verdict, then the warm-up percent -- as a grey
suffix "(37%)". Now:

- One line for the running step (`.job-stage`): its words (`inprogress`), its
  countdown or percent on the right (`statusupdate`), a native `<progress>` under it,
  indeterminate until a percent arrives. A late tick of an earlier step is ignored.
- The full step list is folded under "Show steps" (`<details class="job-steps">`), shown
  only once there is a step.
- Cards (`custom`) and verdicts (summaries) go to `.job-results`, outside the fold, and
  take the line's place. On `close` with nothing rendered the line stops spinning; a
  rendered player's line goes on until the player hides the log (app/action.js).
- A screen reader hears each new step and each verdict once -- a card by its heading,
  and only a heading: the download card has none and starts with a `<script>` holding
  the link, api-key and token (`[data-job-announce]`, role=status), never the
  per-second ticks.
- A job's tag is its step's own words (services/job: `InProgress` tags by the message),
  so progressLog never builds a selector from it (`byAttr`): an apostrophe ("Getting the
  torrent's details", French "l'URL du flux", Italian "dell'inizio") made
  `querySelector` throw and the message, a card included, was lost.
- The words are the `job.*` keys, rewritten as stages in all 11 locales on 2026-10-14
  ("Loading the start of the file", status "looking for peers · 34 s left", then "37%";
  "Speed test, 50 MB" for the bandwidth check; "Checking the file format", which audio
  and images go through too). Past the countdown with nothing yet the status is
  "looking for peers" with no deadline, and seconds round up -- never "0 s left". The
  magnet wait on the home page keeps its short "43 s" (`job.magnetCountdown`, m:ss from
  a minute, both rounded up).
  Only action.go and hls.go use them; nothing (agents, alerts) matches their text.
- Turnstile's check (`lib/turnstileAction.js`) draws the same line; when Cloudflare wants
  a click it says "Tick the box to continue" (`data-label-interactive` on
  `#turnstile-action`) and stops spinning: the wait is the viewer's then.
- Every log on the site looks like this one (owner, 2026-10-18): Download/Watch and the
  TAR/ZIP dialog, the magnet load on the home page and in Discover's stream modal, the
  crypto payment wait, sign-in, the login-link check and sign-out. Their hosts render
  `partials/job_log.html`; `StreamModal.jsx` draws the same markup (a test in
  `lib/progressStage.test.js` compares the classes), a retry in the modal swaps in the
  server's. Their words were rewritten as stages too ("Getting the torrent from peers",
  "Checking the payment", "Logging out"; provider and network errors no longer
  lowercased). A job's `info` line -- "we don't have this torrent yet", "still waiting,
  you can close this page" -- is a note under the line (`.info-summary`, the line stays).
  Until 2026-10-18 nobody ever saw one: services/job tags an info with the step before
  it, and the log took it for a repeat of that step and dropped it. Only the embed keeps
  the plain log: off on prod from 2026-11-01, and its own stylesheet.

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
