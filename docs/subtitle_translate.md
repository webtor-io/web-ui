# Subtitle translation (auto subtitles, phase 2)

AI-translated subtitle tracks in the stream player. Phase 1 (parent spec
`docs/superpowers/specs/2026-09-12-auto-subtitles-design.md`) picked the best *existing* subtitle
for a viewer; phase 2 adds a machine-translated track when no human one exists in the viewer's
preferred language, produced by a separate service (`subtitle-translate`) reached through
torrent-http-proxy as a URL mod.

Design spec: `docs/superpowers/specs/2026-09-13-subtitle-translate-design.md` (decisions 1–13,
amended 2026-09-14). Plans: `docs/superpowers/plans/2026-09-13-subtitle-translate-webui.md` (this
repo), `docs/superpowers/plans/2026-09-13-subtitle-translate-service.md` (the service). Service
contract: `subtitle-translate/README.md`.

## The ladder

`handlers/action/helper.go` ranks every subtitle candidate with `ladderRank`; the player never
reimplements the order, it only reads `data-rank`.

| Rank | Provider (Go)           | Badge (`data-badge`) | i18n key                   |
|------|--------------------------|-----------------------|-----------------------------|
| 0    | `UserSubtitle`           | `user`                | `action.stream.badge.user`     |
| 1    | `MediaProbe` (embedded)  | `embedded`            | `action.stream.badge.embedded` |
| 2    | `ExportTag` / `External` | `sidecar`             | `action.stream.badge.sidecar`  |
| 3    | `OpenSubtitles`, hash match | `os`               | `action.stream.badge.os`       |
| 4    | `OpenSubtitles`, imdb match | `os`               | `action.stream.badge.os`       |
| 5    | `Translated` (AI)        | `ai`                  | `action.stream.badge.ai`       |
| 6–8  | reserved (phase 3 whisper takes 6) | —           | —                            |
| 9    | anything else / "None"   | —                     | —                            |

A track marked **forced** (signs-only) always gets badge `forced` regardless of provider
(`badgeFor`, `action.stream.badge.forced` = "signs only") — the origin is less useful to the
viewer than the fact that it's forced. Forced tracks are listed in any language (spec amended
2026-09-14: no language filter) but are never a candidate for auto-selection, and never the source
of a translation (`isHumanFull`, `pickTranslationSource`).

## Default-track rules

Order of evaluation in `applyLadder` (only runs when `SubtitleOpts.PreferredLang != ""`, i.e. the
feature is on and this is not an embed — see *Gating and flags*):

1. **Saved choice wins.** `VideoStreamUserData.SubtitleID` is honored as-is, *unless* it points at
   a `Locked` item (an AI track saved while paid, now lapsed, or a stale link) — a locked default
   would show "on" with nothing rendered, so it's ignored and the ladder decides fresh.
2. **Explicit caller default.** An embed that pre-selected a track (`ExternalData`) keeps it.
3. **Audio-language rule.** If the base language of the *active* audio track (the `Default` item of
   `GetAudioTracks`, i.e. the Accept-Language match — not the probe's first stream) equals the
   preferred language, full subtitles are **not** turned on and no translation starts; only a
   forced track in the preferred language turns on by default (labeled "signs only"), else "None".
   Unknown audio language counts as "needed".
4. **Ladder for the preferred language.** Otherwise, the best human track (ranks 0–4) in the
   preferred language wins; failing that, if translation is offered (`Translate=true`,
   `Paid=true`), the `Translated` item becomes default instead — never a `Locked` one.
5. **Phase-1 fallback on a ladder miss.** If the preferred language yields nothing activatable
   (NSFW, free viewer facing a locked item, or the language is outside
   `stremio.LanguageByCode`), `applyLadder` falls back to the old phase-1 selection
   (`selectListItem`/`matchLang`: Accept-Language, then English) instead of "None" — the ladder
   must never take away subtitles phase 1 would have turned on.

Switching audio tracks in the modal re-runs the same rule client-side (`pickDefaultSubtitle` in
`subtitle-rules.js`), reading `data-rank`/`data-srclang`/`data-forced` instead of recomputing
anything. Difference from the server: on a miss, the client keeps the current default instead of
recomputing an Accept-Language match (no such matcher client-side) — the server's phase-1 fallback
only applies at page render.

## Preferred language

`streamprefs.Service.PreferredContentLang(ctx, user, uiLang)`:

- Signed-in user with `stremio_settings.preferred_language` set to a language `stremio.LanguageByCode`
  recognizes → that language.
- Otherwise the UI language (`c.Lang`), reduced to its base tag (`ResolvePreferred`).
- Anonymous viewers always get the UI-language path (no DB lookup).

The same function is meant to back audio-track defaulting and the Stremio addon later (phase 5) —
not implemented yet.

## Gating and flags

| Flag | Env var | Effect |
|---|---|---|
| `subtitle-translate-enabled` | `SUBTITLE_TRANSLATE_ENABLED` | `TranslateEnabled()`; off by default. Master switch for offering the AI item at all. |
| `subtitle-translate-free` | `SUBTITLE_TRANSLATE_FREE` | `FreeForAll()`; when set, every viewer may activate the AI track (for deployments without `claims-provider`). |

**The flag is a master switch, not an AI-item gate.** `subtitleOptsFor`
(`jobs/scripts/translate_opts.go`) returns the zero `SubtitleOpts` — `PreferredLang == ""` — when
the feature is off *or* the request comes from the embed widget, and `GetSubtitles` reads an empty
`PreferredLang` as "take the phase-1 path": `selectListItem`/`matchLang` only, `applyLadder` never
runs. So with the flag off a deployment keeps phase-1 selection unchanged, and embeds always use
phase-1 selection. Two things are *not* gated, because they are not part of the selection rule:
**forced tracks stay visible** in any language with the `forced` badge (`badgeFor` runs for every
item), and `matchLang` keeps skipping forced tracks. `jobs/scripts/action.go` also skips the reads
that only feed the ladder in that case — preferred language, `resource_metadata`, TMDB credits —
so the flag costs nothing when off.

`buildSubtitleOpts` (`jobs/scripts/translate_opts.go`) is then reached only with the feature on and
outside an embed; it keeps its own `adult`/`embed` gates as defence in depth and combines the flags
with two more gates:

- **Tier.** `Paid = FreeForAll() || isPaidForTranslate(c)`, where the latter is true iff
  `c.Claims.Context.Tier.Id != 0`. A non-paid viewer still gets the list item (so it's visible as
  an upsell) but `Locked=true` and no `Src` — the URL itself is the entitlement, so a free viewer
  must never receive it, not even hidden in markup.
- **NSFW.** `Translate = enabled && !adult && !embed`. `adult` comes from
  `streamprefs.IsAdultResource` (`resource_metadata.is_adult`; unknown resource or a DB error reads
  as not-adult, matching the existing adult-classification convention — the flag only ever removes
  a capability, never grants one). When adult, there is no AI item at all: no ladder entry, no
  lock, no CTA.
- **Embed widget.** `embed` is `dsd != nil` (an embed `DomainSettingsData` is present). Embeds never
  get the `Translated` item in phase 2 — a third-party page has no `/donate` to send a locked
  viewer to, and the cost would be charged to a viewer web-ui cannot identify.

Enabling the flag with no `ANTHROPIC_API_KEY` on the `subtitle-translate` service is not silent:
the service answers `501 translation is not configured` and the player surfaces it through
`subtitle-translate-error`.

## The Translated item

Built in `applyLadder` when the ladder misses and a source is available (`pickTranslationSource`):

| Field | Value |
|---|---|
| `ID` | `"tr-" + lang` |
| `Label` | `"<LanguageName> · AI"` (e.g. "Portuguese · AI") |
| `SrcLang` | the preferred language code |
| `Provider` | `"Translated"` |
| `Badge` | `"ai"` |
| `SourceBadge` | the source track's badge (`user\|embedded\|sidecar\|os`) — rendered as a muted "from &lt;origin&gt;" suffix, i18n key `action.stream.translate.from` |
| `SourceID` | the source track's `ListItem.ID` — diagnostics only, never rendered or reported |
| `Src` | `api.TranslateURL(src.Src, lang, opts.Names)` when `Paid`, else `""` |
| `Locked` | `true` when not `Paid` |
| `Rank` | 5 |

`TranslateURL` (`services/api/translate_url.go`) appends `~tr:<lang>/<name>.vtt` to whatever
subtitle URL web-ui already built for the source track (sidecar `~vtt`, OpenSubtitles `~vi`,
user-upload `/ext/…~vtt`) and adds `?names=<csv>` when a glossary is present — the proxy resolves
the inner chain itself.

**Translation source selection** (`pickTranslationSource`): a non-forced, URL-backed human track,
preferring the active audio's language (transcribing what's said, not translating a translation),
then English, then any. Embedded (`MediaProbe`) tracks are never a source — they have no
standalone URL for the proxy chain to fetch.

**Glossary.** `streamprefs.CastNames(ctx, imdbID, 30)` reads up to 30 cast names from TMDB credits
stored by enrichment, passed as `names=` to the service.

**Key stability.** The service's cache key is `sha256(InfoHash + Path + lang + model +
promptVersion)` — it does **not** include `names` or any per-viewer query parameter. The artifact
is shared by every viewer of that track/language, so **the first requester's glossary is baked
into the cached translation for everyone else too** (service README, "What survives the round
trip" / cache-key section).

## Client behaviour (`Player.jsx`, `subtitle-rules.js`, `subtitle-progress.js`, `cue-offset.js`)

- **Audio switch re-pick.** `onAudioSelect` calls `pickDefaultSubtitle(readTracks(modal), audioLang,
  preferredLang)` and activates the result, unless the viewer already made a manual subtitle choice
  this session (`manualSubtitleRef`) — re-picking over an explicit choice would read as the player
  fighting the viewer. `manualSubtitleRef` is also seeded on mount from a `data-saved` default
  (`ListItem.Saved`, set where `ud.SubtitleID` wins): a choice the viewer saved in an earlier
  session is as explicit as one made in this one. The seed reads `readAllTracks`, not `readTracks`:
  a saved **"None"** is a choice too, and `readTracks` drops the `none` entry — missing it turned
  subtitles back on over an explicit off. *(Spec says the audio element carries `data-audio-lang`; the shipped code
  instead reuses the existing `data-srclang` attribute on `.audio` items — see ledger.)*
- **Only deliberate activations are persisted.** `activateSubtitle(container, item, {persist})`
  passes `persist` through to `markTrack`, which is what issues the `PUT /stream-video/subtitle`
  that becomes `ud.SubtitleID`. Clicking a list item and uploading a file persist; the
  engagement-gate AI auto-start and the audio-switch re-pick do not. `Saved` therefore means
  exactly "the viewer chose it", and a rule the player applied on the viewer's behalf never comes
  back next page load as a choice that switches the rule off.
- **Lock → CTA.** Clicking a `Locked` item never activates it (no `Src` to activate); it reveals
  `#translate-cta` (the `/donate` link, event `donate-subtitle-translate`) and fires
  `subtitle-translate-lock-click {lang}`.
- **Progress polling.** `pollProgress` issues a `HEAD` on the track's `src` every 3 s (`HEAD` never
  starts a job). `X-Subtitle-Progress: done/total`; `0/0` means "not registered yet, keep polling"
  (never final). Done ⇔ `total > 0 && done >= total`. A non-200 response calls `onError` once and
  **stops the poll** — it does not retry.
- **`rev` reload (throttled).** A reload rewrites the `<track>` `src` via `withRev`
  (adds/replaces `?rev=<done>`, every other query param — including the signed token — passes
  through untouched), forcing a refetch. The browser drops the cue list while it reparses, so the
  track is briefly empty on every swap: reloads are therefore throttled to at most one per
  `TRACK_RELOAD_INTERVAL_MS` (15 s, `Player.jsx`) and always happen on the final progress, while
  the `.tr-progress` percentage still updates on every 3 s poll. No reload happens while
  `total === 0` (nothing to fetch yet). `reloadSubtitleTrack` (`subtitle-track-reload.js` — its own
  module so the listener lifetime is testable; `node --test` cannot parse `Player.jsx`) returns
  whether it actually swapped the `src`, and **only a real swap stamps the throttle** — a no-op
  must not hold off the next real reload for another 15 s.
  `captureTrackState`/`restoreTrackState` (`cue-offset.js`) snapshot and restore cues/mode around
  the swap, and the same snapshot is restored if the new revision fails to load (`error` on the
  `<track>` → `subtitle-translate-error {code:'track'}`, once per run); session cue-offset
  (mid-movie resume) reapplies via the existing `load` listener. There is **one `load`/`error`
  listener pair per `<track>` at a time**: the pending pair is removed before the next revision's
  goes on, so a failure restores the *latest* snapshot rather than one from several revisions ago.
  `onTrackError` additionally checks the run identity (`runSeqRef`): a `<track>` whose `src` a
  stopped run set can still fail afterwards, and that event must not kill the current run.
- **Progress text.** `player.subtitleTranslating` = `"Translating… %v%"`, next to the active AI
  item (`.tr-progress` span), hidden again on done, on error, and whenever the poll is stopped
  (`stopTranslationProgress` holds the span in `progressSpanRef`) — a span left visible freezes at
  the last percent and reads as a translation stuck forever.
- **Auto-start.** Translation does **not** start on page load and there is no dedicated 5-second
  timer: it piggybacks on the existing `stream-start` engagement gate, which fires once
  `state.currentTime >= 5` (5 s of actual playback, not wall-clock time since load). If the AI item
  is still the default at that point (not overridden by a manual pick) and not locked, the player
  activates it and starts the progress poll — matching the spec's "starts 5 seconds into viewing."

## Telemetry (Umami)

| Event | Fields | Notes |
|---|---|---|
| `subtitle-resolved` | `level` (`'0'`–`'5'`/`'none'`), `hasUiLang`, `count`, `badge`, `needed`, `translated`, `uiLang`, `audioLang` | Fires on the `stream-start` gate (playback ≥ `ENGAGEMENT_SECONDS`). `needed = audioLang base != preferred content language base` (`data-preferred-lang`, falling back to the UI language when unset; unknown audio ⇒ needed). `translated = badge === 'ai'`. Level `'5'` = AI translation; `'6'` reserved for whisper (phase 3), not emitted yet. |
| `subtitle-select` | `provider`, `srclang`, `source`, `badge` | `badge` is an additive field vs. phase 1's schema. |
| `subtitle-translate-start` | `lang`, `source` | `source` = the item's `data-source-badge` (`SourceBadge`), i.e. what human track is being translated. |
| `subtitle-translate-done` | `lang`, `seconds`, `cues` | `seconds` = wall time since start, rounded to 0.1; `cues` = last `total` seen. |
| `subtitle-translate-error` | `lang`, `code` | `code` = HTTP status, `0` network error, `'track'` the reloaded `<track>` failed to parse/load, `'timeout'` the run passed `POLL_TIMEOUT_MS`. |
| `subtitle-translate-lock-click` | `lang` | Free viewer clicked the locked AI item. |
| `donate-subtitle-translate` | (button attrs: `data-umami-event-tier=free\|anon`) | CTA inside the lock card. |

Disagreement with spec: the design spec (line ~219) also calls for a "bad translation" variant of
the existing `report-problem` control with `data-provider=Translated`. **Not implemented** — no
code path sets that variant. Flag for the controller; not tracked in `rulings.md` or `progress.md`
as descoped.

## Template attributes (`data-*` per list-item kind)

| Kind | Where | Attributes |
|---|---|---|
| `.audio` `<li>` | `#embedded` audio column | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-default` |
| `.subtitle` `<li>` | `#embedded` subtitles column (`$otherSubs`: embedded/sidecar/AI, excludes OpenSubtitles & user uploads) | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-src`, `data-label`, `data-kind`, `data-badge`, `data-source`, `data-rank`, `data-source-badge` (Translated only), `data-forced`, `data-locked`, `data-default`, `data-saved` |
| `.subtitle` `<li>` | `#opensubtitles` (`$openSubs`) | `data-id`, `data-provider`, `data-srclang`, `data-source`, `data-src`, `data-label`, `data-kind`, `data-badge`, `data-rank`, `data-default`, `data-saved` (never `forced`/`locked`/`source-badge`) |
| `.subtitle` on a `<div>` inside `<li>` | `#my-subtitles` (`templates/partials/action/user_subtitles.html`) | `data-id`, `data-provider="UserSubtitle"`, `data-src`, `data-label`, `data-srclang`, `data-badge="user"`, `data-rank="0"` (fixed — this view model has no ladder), `data-default`, `data-saved`, `data-autoselect="true"` when just uploaded (separate mechanism from `data-default`, unrelated to the ladder) |

`data-default`/`data-saved` on the uploads list come from `UserSubtitleTrack.Default`/`.Saved`,
copied by `Helper.UserSubtitleView` out of the matching `ListItem` (`us-<uuid>`) of the same
`GetSubtitles` call the modal renders from — the tab has its own view model, so without that copy
the player's audio-switch rule read every upload as "nothing chosen" and could switch away from a
subtitle the viewer had uploaded and picked. The async reload after an upload
(`handlers/user_subtitle`, `buildView`) has no ladder result to copy from and marks only the
just-uploaded row (which the player activates and persists immediately); a reload for any other
reason — a delete — marks nothing, and `syncMySubtitleMark` in `Player.jsx` re-derives
`data-default` from the live `textTracks`.

`readTracks` (`subtitle-telemetry.js`) matches all three list-item shapes via the `.subtitle[data-provider]`
selector, filtering out `data-id="none"`.

## Known limitations

- **Job cache key ignores tier/preferred-language changes.** The 10-minute streaming-job cache key
  (`jobs/scripts/action.go`, `Action`) is built from resource/item/action/role/settings/audio+
  subtitle choice/`c.Lang`/session — it does **not** include paid-tier status or
  `stremio_settings.preferred_language`. A viewer who upgrades their plan or changes their
  preferred language may keep seeing the old `SubtitleOpts` (locked item, or old language) until
  the current bucket rolls over. Parked as a follow-up, not fixed in this task.
- **Embedded-only source files get no AI item.** If every text-subtitle candidate is a `MediaProbe`
  (embedded) stream, `pickTranslationSource` finds no URL-backed source, so no `Translated` item is
  added regardless of how many embedded tracks exist.
- **Translations of embedded HLS subtitle tracks are not supported.** Same root cause, from the
  source side: embedded tracks are served through the transcoder's HLS subtitle group with no
  independent URL for the `~tr:` mod to attach to. Deferred to phase 3+ in the spec.
- **Each reload refetches the whole partial VTT.** The `rev` reload swaps the `<track>` `src` and
  lets the browser reparse from scratch — no incremental cue-append; the file is just short early
  on and grows with each revision. The 15 s throttle bounds the cost (and the blank-cue window) but
  does not remove it: the subtitle text catches up in steps while the percentage moves smoothly.
- **A translation is reported once per item per page load.** `translationAction(track, status)`
  (`subtitle-rules.js`) reads a per-item status map (`'running'`/`'done'`) and answers `'start'`,
  `'resume'` or `'none'`. `'start'` emits `subtitle-translate-start`; `'resume'` polls again
  **silently** after the viewer selected another track mid-run and came back (same translation, so
  no second start event — `done` still fires once, with the wall time since the *first* start);
  `'none'` covers a finished item, a locked one and anything that is not an AI track, so a warm
  cache cannot fire a second start/done pair via the engagement-gate auto-start. Re-selecting the
  item whose poll is running right now is also a no-op (`pollingIdRef` in `Player.jsx`) rather than
  a self-inflicted stop.
- **After an error, the item stays `'running'`.** Re-selecting it resumes (a silent retry) rather
  than reporting a fresh start; each attempt can still emit its own `subtitle-translate-error`.
- **Polling gives up after 15 minutes** (`POLL_TIMEOUT_MS`, `subtitle-progress.js`) with
  `subtitle-translate-error {code:'timeout'}`; a run that outlives the cap is treated as gone.

## Testing

- Go: `make test` (not bare `go test ./...` — the proto registration conflict between the
  abuse-store and torrent-store protobufs panics test binaries without
  `-ldflags -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore`, which the
  Makefile target sets). Relevant packages: `handlers/action` (ladder, `badgeFor`, `applyLadder`
  negative controls), `models` (`SubtitleOpts`), `services/streamprefs`, `services/api`
  (`translate_url_test.go`), `jobs/scripts` (`translate_opts_test.go`).
- Render guard: `services/template/stream_video_render_test.go` renders
  `templates/views/action/stream_video.html` with a minimal `StreamContent` so an arity mismatch or
  nil-field panic in `getSubtitles`/`getAudioTracks` template bindings goes red under `make test`
  instead of only surfacing at runtime (the template funcs are bound by reflection; arity is
  checked at `tm.Init()`, not compile time). `services/template/user_subtitles_partial_render_test.go`
  covers the upload partial.
- JS: `npm test` (`node --test`, plain-JS modules only — cannot parse JSX, so `Player.jsx` itself
  isn't covered, only the modules it imports). Covers `subtitle-rules.test.js`
  (`pickDefaultSubtitle`, `baseLang`, `translationAction`, `hasSavedDefault`),
  `subtitle-progress.test.js` (`parseProgress`, `withRev`, `pollProgress`),
  `subtitle-telemetry.test.js` (`readAllTracks`, `readTracks`, `selectEventData`,
  `resolveSubtitleLevel`), `subtitle-track-reload.test.js` (`reloadSubtitleTrack`: listener
  lifetime, latest-snapshot restore, the did-it-reload return value).
