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
| 4    | `OpenSubtitles`, imdb match | `os` + hint        | `action.stream.badge.os`       |
| 5    | `Translated` (AI)        | `ai`                  | `action.stream.badge.ai`       |
| 6–8  | reserved (phase 3 whisper takes 6) | —           | —                            |
| 9    | anything else / "None"   | —                     | —                            |

**When the OpenSubtitles fetch fails, the page still renders** — without those rungs.
`api.GetOpenSubtitles` checks the HTTP status before it decodes: a non-200 is a typed
`api.StatusError` naming the status, and the job log shows that instead of the decoder's
"unexpected end of JSON input", which is what an error page, an empty 502 or a 429 used to
surface as. A `Retry-After` on a non-200 rides along in that error and is **reported, never
obeyed** — no retry loop, no sleeping out somebody else's back-off inside a job step a viewer is
watching. A 200 with an empty body is an empty list: the file has no subtitles.

**A 200 that carries a `Retry-After` is the one answer that is neither.** video-info sends it
while one of its search legs is still waiting on the seeder — the body is an empty list, but the
lookup never finished. Reading it as "no subtitles" is worse than the 404 it replaced: the stream
job's rendered result is cached for ten minutes, so one early answer takes OpenSubtitles away
from every viewer of that file for the rest of the bucket, with the job step marked done.
`GetOpenSubtitles` answers `api.SubtitlesNotReadyError` (sentinel `api.ErrSubtitlesNotReady`,
carrying the seconds; a non-delta-seconds header means "not ready, no usable hint" and zero), and
`fetchOpenSubtitles` (`jobs/scripts/opensubtitles.go`) retries **once** after the `Retry-After`,
capped at 5 s — and 5 s when the header is absent or unreadable, since the service did say it
needed time — never past the step's own deadline.

Still not ready, and the page renders without those tracks. Two things then happen. The run calls
`job.Job.DoNotCache()`, which retires the result instead of leaving it in storage for every later
request with the same id: the job queue is also a ten-minute cache, and serving this one page to
the rest of the bucket would hand the gap to viewers who would have got the tracks. (Retiring is
the same path a failed run already takes, 60 s after the run ends, so a viewer still replaying the
log is unaffected.) And the render says so: a `Warn` in the log, `data-subtitles-not-ready` on the
picker, `notReady` on `subtitle-resolved` — without which a level of `'none'` counts a file nobody
looked at as a file with nothing to find.

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

**No preferred language means no re-pick.** `data-preferred-lang` is empty in two live
configurations — every embed, and any deployment with `SUBTITLE_TRANSLATE_ENABLED` off
(`subtitleOptsFor`, `jobs/scripts/translate_opts.go`, returns the zero `SubtitleOpts` for both) —
and there the rule has nothing to decide *for*, so it keeps the current selection rather than
answering `none`. It used to answer `none`, which switched subtitles off behind a first-time viewer
who touched the audio menu, with `persist:false` so nothing recorded why (fixed 2026-09-16; wiring
test in `Player.wiring.test.js`).

**The language table is a superset of the service's — and identity is not detection.**
`applyLadder` offers a translation only when `stremio.LanguageByCode(lang) != nil` (an item
leading to a rejected request is worse than no item), so `stremio.Languages` has to contain every
code `subtitle-translate` accepts. Twelve did not (`sk lt lv et fa bn ta kk ka hy az ca`) and were
appended 2026-09-16; `TestLanguagesCoverTheTranslateService`
(`services/stremio/lang_superset_test.go`) embeds the service's list and says what is missing,
with the refresh procedure in the comment above it.

**Nobody was locked out before that, though** (review I1): `PreferredLang` comes from
`stremio_settings.preferred_language`, which the settings handler validates against this very
table before storing, or from `c.Lang`, one of the eleven shipped UI locales — all of which were
already here. The twelve rows do not recover a cohort that was being refused; they create the
possibility of one, starting with twelve new entries in the Stremio settings dropdown. Worth
knowing before the list grows again.

**The table answers four questions and only two of them wanted the new rows.** *What is this
language called* (display) and *will the translate service accept it* (the ladder gate) do.
*What tokens in a title mean it* (detection) does not, and that is where the first version did
damage: `KAT` is KickassTorrents branding, `EST` is the Electronic-Sell-Through tag, and
`Фильм 2019 [KAT] 1080p` came back **Georgian** instead of Russian — `ExtractLanguages` runs its
Cyrillic fallback only when nothing else matched, so a false positive *pre-empts* the right
answer, and `LangFilterStream` is exclusive, so that release left a Russian viewer's Stremio list
without a word.

So `Language.TitleAliases` is now the detection half, separate from `Code`/`Name`/`Flag`, and
`langMap`/`ExtractLanguages` are built from it alone — flag included, since a flag is just
another token. All twelve appended rows carry **none**: nameable, choosable, never detected, until
someone measures tokens against real release vocabulary.
`TestAppendedLanguagesAreNotDetectedFromTitles` is the review's measured table, including the
Cyrillic row.

Two consumers had to learn the same rule, because filtering *by* an undetectable language keeps
nothing and reads to the viewer as "there is nothing for this film":
`filterStreamsByLanguage` (`lang_filter_stream.go`) and `matchesPreferences`
(`services/release_subscription/poller.go`) both treat "resolvable but not detectable" exactly as
they already treated "unknown code" — not a filter. The client needs no such guard:
`matchesPrefs` (`streamPrefs.js`) already keys on a name it could not resolve and answers "not a
filter", and Discover's chips are built from what the streams themselves advertise.

The table is mirrored in `assets/src/js/lib/discover/lang.js`, and since 2026-09-16 that mirror is
**enforced**: `TestLangJSMirrorsTheGoTable` parses the JS file and compares `code`, `name`, `flag`
and `titleAliases` row by row. Excluded: `extraFlags` (client-only) and the single code-less row,
`Latino`, which is a release-title tag rather than a language anyone can choose — the test asserts
it is the *only* one. That row also shares Spain's flag, which used to shadow Spanish in
`LANG_MAP`; registration there is first-wins now, so a bare 🇪🇸 resolves to Spanish and 🇲🇽/🇦🇷
still reach Latino.

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

**Preview as a free viewer.** Append `&debug=tier:free` to the resource hash
(`#action=stream&debug=tier:free`): `previewAsFree` (`jobs/scripts/translate_opts.go`) clears
`Paid` on the computed options, so a paid account sees the AI track locked with the CTA. The value
only ever downgrades, which is why `handlers/action/handler.go` lets it through under
`GIN_MODE=release` while every other `debug` value stays dev-only; it is part of the job cache
key, so the preview never contaminates the paid render.

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

**The source URL must be absolute.** The output is built as `scheme://host` + path, so a relative
or scheme-less source would come out as `://…` or `https:///…` — a string that looks like a URL
and is not one. `TranslateURL` returns `""` for those, and `applyLadder` then **drops the
Translated item entirely** rather than offering an unlocked track with no `Src` (subtitles
"selected" with nothing on screen). Every subtitle URL web-ui actually builds is absolute, so this
is a guard, not a routine path.

This leaves a deliberate asymmetry: the drop is keyed on `Src == ""` *and* `!Locked`, so a **free**
viewer still sees the locked AI item for a source that could never have produced a URL. That is
correct rather than an oversight — the locked item is an upsell, not a track; it never carried a
`Src` and never will, and hiding it would make the CTA's presence depend on a property of a source
the viewer cannot see. The cost is that such a viewer could upgrade and then find no AI item at
all, which is the same outcome every unsupported source already gives (see *Known limitations*).

**Translation source selection** (`pickTranslationSource`): a **file** source — the viewer's own
upload, an `ExportTag` sidecar, an OpenSubtitles track — in the preferred language order (the
active audio's language, transcribing what's said rather than translating a translation, then
English, then any) beats an **embedded** one (`MediaProbe`, the live playlist). The embedded
bucket is reached only when no file source qualifies **at all** — not per language: a French
sidecar is picked over a Russian live playlist with Russian audio (owner ruling, 2026-09-16).

The cost of the two is not comparable, which is what the ruling is about. A file is one fetch of
seconds-to-minutes and leaves a final artifact cached under `ArtifactKey` for every later viewer.
The live source runs at transcode speed, holds one of the service's `--live-max-jobs` slots for
the length of the film, stops when the viewer leaves, and caches a final only for a contiguous run
from offset 0. Until the ruling the winner fell out of the order `GetSubtitles` appends in
(`MediaProbe` first, then `ExportTag`, `OpenSubtitles`, `External`, `UserSubtitle`), so a
transcoded file translated its own live playlist even when the viewer's own upload sat in the same
language — while the display ladder ranks that upload *above* embedded. The two orders disagreeing
was a bug, not a policy.

**Among file sources of the same language** (`translationSourceRank`): the viewer's own upload,
then a **hash-matched** OpenSubtitles track, then the sidecar, then an **imdb-matched**
OpenSubtitles track. This is deliberately **not** `ladderRank`, which puts the sidecar above both
OpenSubtitles rungs: the ladder answers "which track does the viewer want to read", this order
answers "which text will the machine translate correctly", and a hash match is a match on this
very file while a sidecar shipped in the torrent is unverified and routinely belongs to another
release. Equal ranks keep list order, which is the only stable tie-break the embedded bucket has.

Embedded (`MediaProbe`) tracks are a source when the stream plays through the transcoder:
`GetSubtitles` gives each visible embedded track `Src = <HLSSessionBase>/s<MPID>.m3u8` (the same
variant hls.js plays; `SubtitleOpts.HLSSessionBase` is set in `streamContent` once the session
exists), and `TranslateURL` appends `~tr:<lang>/s<MPID>.vtt` to it. The service then follows the
live playlist (service README, «Live HLS source»): the translation arrives along with the
transcode, `X-Subtitle-Live: 1` marks it as still growing, and a final artifact is cached only
for a contiguous run from the start. A native MP4 played without a session has no base and no
embedded source, as before — and a list with neither a file source nor a session offers no
translation at all.

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
  audio-switch re-pick does not. `Saved` therefore means
  exactly "the viewer chose it", and a rule the player applied on the viewer's behalf never comes
  back next page load as a choice that switches the rule off.
- **Lock → CTA.** Clicking a `Locked` item never activates it (no `Src` to activate); it reveals
  `#translate-cta` (the `/donate` link, event `donate-subtitle-translate`) and fires
  `subtitle-translate-lock-click {lang}`.
- **Progress polling.** `pollProgress` issues a `HEAD` on the track's `src` every 3 s (`HEAD` never
  starts a job). `X-Subtitle-Progress: done/total`; `0/0` means "not registered yet, keep polling"
  (never final). Done ⇔ `total > 0 && done >= total`. A non-200 response calls `onError` once and
  **stops the poll** — it does not retry.
- **`X-Subtitle-Live` (embedded sources).** `parseProgress(header, live)` also reads
  `X-Subtitle-Live: 1` off the same `HEAD` response: while it is set, `total` is a snapshot of the
  live source playlist, not a ceiling, so `final` is forced `false` even when `done >= total` — only
  the header's disappearance (the underlying transcoder session ended or went idle) lets the normal
  `total > 0 && done >= total` rule apply again, and the poll keeps running instead of stopping
  early. The chip reflects this: `Player.jsx`'s `onProgress` shows a bare count (`· <done>`, no
  denominator worth a percentage) with `title = tf('player.subtitleTranslatingLive')` while live,
  instead of the usual `· <pct>%` / `player.subtitleTranslating`.
- **`X-Subtitle-Status` (the service's verdict).** The same `HEAD` response can carry
  `X-Subtitle-Status: done` or `stopped`, and it outranks the counts because it knows two things
  they cannot say. `done` means the live source ended and everything in it was translated — a
  live run can finish with no final artifact to cache (a seeked session caches nothing), and
  without the header that is indistinguishable from a playlist that simply has not grown in the
  last three seconds, so the run polled on to the 15-minute cap. `parseProgress(header, live,
  status)` therefore forces `final: true` on `done` **regardless of `live`**. `stopped` means the
  run ended incomplete (`source_gone`, `too_large`), which on the counts alone looks exactly like
  a job that is merely behind: `pollProgress` calls `onError('stopped')` once and stops, after
  reporting that response's counts — they are the last ones there will be, and those cues are on
  screen. The player then **keeps the chip's count** and hides only the spinner, with
  `title = player.subtitleTranslationStopped` ("Translation stopped — reload to retry"), and emits
  `subtitle-translate-error {code:'stopped'}`. Every other failure clears the count, because a
  frozen percentage reads as a translation still running. A response without the header behaves
  exactly as before, which is what every batch run and every pre-2026-09-16 service sends.
- **The poll sleeps with the video.** On the `<video>`'s `pause` event and on
  `visibilitychange` → hidden, `Player.jsx` suspends the running poll (`stop.suspend()` on
  `pollProgress`'s controller); `play`, and a `visibilitychange` back to visible on a video that is
  not paused, resume it (`stop.resume()`). The HEAD every 3 s is what tells the service somebody is
  watching (`--live-idle` 90 s), and for a live source it is also what keeps the transcoder session
  and its FFmpeg run alive — a paused or hidden viewer would otherwise pay for a whole film nobody
  is watching. **Suspending is not stopping:** no `onError`, no telemetry, no second
  `subtitle-translate-start` on the way back (the item stays `'running'`, so `translationAction`
  still answers `'resume'`), and the chip keeps its count and its spinner — unlike
  `stopTranslationProgress`, which hides both. Time spent asleep does not count against
  `POLL_TIMEOUT_MS` either: the deadline moves forward by the length of the sleep, so an hour on
  pause does not come back as a timeout. Resuming is cheap on the service side too — partial
  progress is held 24 h and re-aligned by cue identity. The listeners are removed on unmount
  (the effect's cleanup, reached through `destroyPlayer`).
  **The initial state is read too**, but on the *first response*, not before it: a run begun on a
  video that has never played (autoplay blocked, or the mount-time restore of a saved AI track) or
  in a tab that was already in the background gets no `pause` and no `visibilitychange` to sleep
  on, so `suspendIfNobodyIsWatching` in the first `onProgress` puts it to sleep. It fires **only
  for a live source** (`X-Subtitle-Live`), which is the only one that costs anything to keep
  awake — a transcoder session and its FFmpeg run. The first HEAD is what reveals that, which is
  why the check waits for it: before 2026-09-16 it ran at once and also froze a cached
  OpenSubtitles run — seconds of work, no session behind it — at `· 0%` with a spinner that did
  not move until the viewer pressed play, and pausing the film to open the picker is exactly when
  people start one. A live run clicked while paused now shows the count its first answer brought
  and polls nothing more until play.
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
- **Waiting for a slot.** `0/0` means "the job has not counted the cues yet", which covers both a
  run that has just started and one queued behind every other translation on the service. After
  `QUEUE_HINT_MS` (30 s) of nothing but `0/0`, `pollProgress` reports once more with
  `queued: true` and the chip reads `· …` with `title = player.subtitleTranslationQueued`
  ("Waiting for a translation slot…") instead of `· 0%`, which reads as a translation stalled at
  the start. It is a display rule and nothing else: the poll keeps running, the timeout deadline
  is untouched, and the first real count clears it — including a `total` appearing while `done` is
  still 0, which is why the report now fires on either number moving rather than on `done` alone.
  Time the poll spends suspended does not count toward the 30 s, the same way it does not count
  toward the timeout: a run asleep is not a run queued. The three things the chip can say (queued,
  live, counting — in that order of precedence) are one pure function, `progressText`
  (`subtitle-progress.js`), so the player holds no chip vocabulary of its own.
- **Progress text.** The AI chip's `.tr-progress` span shows the bare percentage — `· 0%` on
  start, then `· N%` on every 3 s poll. The localized sentence
  `player.subtitleTranslating` = `"Translating… %v%"` is **not** the span's text any more: it is
  written into the span's `title` (same value, refreshed with each percentage), so the chip stays
  the width of a chip while the sentence is still available on hover and to assistive tech. The
  design reason is in `docs/uikit.html` §19 — a chip row cannot carry a sentence per chip.
- **Progress spinner.** A `.tr-spinner` (`loading loading-spinner loading-xs`) sits beside the span
  on the same chip. Both are `hidden` in the markup and are unhidden together when a run starts or
  resumes, and hidden again on done, on error, and whenever the poll is stopped
  (`stopTranslationProgress` holds them in `progressSpanRef`/`progressSpinnerRef` and clears both
  refs). Either one left visible freezes at the last percent and reads as a translation stuck
  forever, so they are always toggled as a pair; `setChipActive` never rebuilds a chip's
  `innerHTML`, so neither element is lost when the viewer switches tracks mid-run.
- **No auto-start** (owner, 2026-09-16). Nothing the player does on its own begins a translation:
  there is no page-load start, and the `stream-start` engagement gate it used to piggyback on
  (`state.currentTime >= 5`) no longer touches the AI item — that effect is telemetry only. A run
  begins on a click of the chip, on the switch restoring `data-last-subtitle`, or on the
  mount-time restore of a translation saved in an earlier session. See "Picker behaviour". The
  spec's old "starts 5 seconds into viewing" is superseded by decision 14.

## Telemetry (Umami)

| Event | Fields | Notes |
|---|---|---|
| `subtitle-resolved` | `level` (`'0'`–`'5'`/`'none'`), `hasUiLang`, `count`, `badge`, `needed`, `translated`, `uiLang`, `audioLang`, `notReady` | Fires on the `stream-start` gate (playback ≥ `ENGAGEMENT_SECONDS`). `needed = audioLang base != preferred content language base` (`data-preferred-lang`, falling back to the UI language when unset; unknown audio ⇒ needed). `translated = badge === 'ai'`. `notReady` (`data-subtitles-not-ready`) says the OpenSubtitles lookup never finished on this render — exclude those rows before reading a `'none'` rate as "files with no subtitles". Level `'5'` = AI translation; `'6'` reserved for whisper (phase 3), not emitted yet. |
| `subtitle-select` | `provider`, `srclang`, `source`, `badge` | `badge` is an additive field vs. phase 1's schema. Fires for every activation the viewer asked for — a chip press **and** the subtitles switch turning them back on (`trackSubtitleSelect`, one call site each); never for `none`, and never for the activation the player performs by itself (the audio-switch re-pick). |
| `subtitle-translate-start` | `lang`, `source` | `source` = the item's `data-source-badge` (`SourceBadge`), i.e. what human track is being translated. **Since 2026-09-16 it cannot fire without an explicit act**: the server never defaults the AI item and the engagement-gate auto-start is gone, so a run begins on a click of the chip, on the switch restoring `data-last-subtitle` (a translation the viewer already ran this session), or on the mount-time restore of one they saved in an earlier session. Rates before and after that date are not comparable — and the two restore paths **do** emit `start`/`done`, as replays of a cached file rather than new work, so the event counts a translation being *shown*, not one being *produced*. |
| `subtitle-translate-done` | `lang`, `seconds`, `cues` | `seconds` = wall time since start, rounded to 0.1; `cues` = last `total` seen. |
| `subtitle-translate-error` | `lang`, `code` | `code` = HTTP status, `0` network error, `'track'` the reloaded `<track>` failed to parse/load, `'timeout'` the run passed `POLL_TIMEOUT_MS`, `'stopped'` the service reported `X-Subtitle-Status: stopped` (`source_gone`/`too_large`) — the one code that leaves the chip's count on screen. |
| `subtitle-translate-lock-click` | `lang` | Free viewer clicked the locked AI item. |
| `donate-subtitle-translate` | (button attrs: `data-umami-event-tier=free\|anon`) | CTA inside the lock card. |

**Telemetry: not comparable across this release.** Do not read a day-over-day or week-over-week
line through the deploy date. `subtitle-resolved.count` grows for reasons that are not "more
subtitles were found" — forced tracks are now listed in any language, and the `Translated` item is
an extra list entry; `subtitle-select` gained a `badge` field (rows before the deploy have none);
and level `'5'` did not exist before, so any level histogram changes shape rather than moving.
Compare within a period on one side of the deploy, or re-baseline.

Disagreement with spec: the design spec (line ~219) also calls for a "bad translation" variant of
the existing `report-problem` control with `data-provider=Translated`. **Not implemented** — no
code path sets that variant. Deferred to a follow-up by controller ruling (the form lives on the
resource page); the spec is amended.

## Template attributes (`data-*` per chip)

Since the picker redesign there is one flat container per group and no sub-views. Every chip is a
`<button type="button">`; the `.audio`/`.subtitle` marker classes and the whole `data-*` set are
unchanged from the `<li>` era, so `readAllTracks`, `readTracks`, `findSubtitleItem` and
`initDefaultTracks` kept working across the change untouched. One reader did change:
`remapTrackGroup` (`hls-manager.js`) matched a manifest track name against the element's
`textContent`, and a chip's text is now decorated (origin code, property tag, source suffix), so
it reads `data-label` — which is why every audio chip carries one (R7). `activateSubtitle` reads
the same attribute for the `<track label>` it creates, and no longer falls back to the chip text
for the same reason.

### Ids (`templates/views/action/stream_video.html`)

| id | element | role |
|---|---|---|
| `#subtitles` | `<dialog class="modal">` | the picker; carries `data-resource-id`, `data-item-id`, `data-preferred-lang`, `data-subtitles-off` (`"true"` while subtitles are off) and, once the viewer has switched them off in this session, `data-last-subtitle` (the id to restore) |
| `#subtitles-toggle` | `<input type="checkbox" class="toggle toggle-soft">` | the subtitles on/off switch, **first child of `#subtitle-langs`** — where the "Off" chip used to be (owner, 2026-09-16) — in a `<label class="flex items-center">` with an `sr-only` name. Checked iff the default item is not `none` |
| `.lang-row` | `<div>` inside `#subtitle-langs` | the language chips, "+N" and the `<template>`. Exists so the muted state can dim the chips without dimming the switch beside them; `applyOffState` writes `.picker-off` here, never on `#subtitle-langs` |
| `#audio-tracks` | `<div role="radiogroup">` | audio chip row |
| `#subtitle-langs` | `<div role="group">` | the switch plus the language row (`aria-label` = `action.stream.subtitleControls`, which names both — the chips alone are a filter, not a choice) |
| `#subtitle-lang-more` | `<button aria-expanded>` | the "+N" disclosure; its whole visible label lives in the single `.more-count` span |
| `#lang-chip-template` | `<template>` | one blank `.lang.lang-chip` that `track-picker.js` clones when an upload introduces a language the server rendered no chip for |
| `#subtitle-tracks` | `<div role="radiogroup">` | subtitle chip row |
| `#subtitle-none` | first child of `#subtitle-tracks` | the `none` list item, since 2026-09-15 a **hidden state carrier** and not a control (renamed from `#subtitle-off`, which read like the button it no longer is): `hidden`, `aria-hidden="true"`, `tabindex="-1"`, no label and no chip classes. The player still activates it by `data-id="none"` (`findSubtitleItem`, `pickDefaultSubtitle`, `hasSavedDefault`, `dropDeletedTracks`); the act of turning subtitles off belongs to `#subtitles-toggle` |
| `#my-subtitles` | `<div class="flex flex-wrap …">` | the async swap target for uploads, a **sibling after** `#subtitle-tracks` (a11y, 2026-09-16): it holds the disclosure and the panel, and a radiogroup holds radios and nothing else. Carries `data-upload-open` (the panel state that survives the swap) |
| `#my-upload-chips` | `<div class="contents">` | rendered **only** on the async reload (`UserSubtitleView.RenderChips`), inside `#my-subtitles`. Wraps that response's MY chips and says "this is the viewer's complete current upload list"; `adoptUploadChips` moves the chips into the radiogroup, reconciles the row's MY set against them and removes the wrapper |
| `#my-uploads-toggle` | `<button aria-controls="my-uploads-panel">` | the dashed "+ My Subtitles" disclosure (label = `action.stream.mySubtitles`), rendered by the uploads partial |
| `#my-uploads-panel` | `<div class="basis-full" hidden>` | heading line (`action.stream.mySubtitles` + the close control), upload form, one row per file, each row with its own delete form |
| `#my-uploads-close` | `<button type="button" class="btn btn-ghost btn-xs">` | the panel's own "×", in its heading line (`aria-label` = `action.stream.close`). Closes the panel exactly as pressing the chip again does — same function in `Player.jsx`, same three writes |
| `#translate-cta` | `<div hidden>` | the locked-AI card, below the track row |
| `#subtitle-hint` | `<p class="text-xs text-w-muted">` | the pending offer in words: this language has no subtitles yet and the translation is there to be started. Visible exactly while some chip is `Offered`, is not the track playing, and its language holds nothing else activatable. Between the track row and the CTA card |

### Chips

| Kind | Where | Attributes |
|---|---|---|
| `.audio` | `#audio-tracks` | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-label`, `data-lang`, `data-lang-name`, `data-lang-flag`, `data-default` |
| `.subtitle#subtitle-none` | first child of `#subtitle-tracks`, hidden | `data-id="none"`, `data-provider=""`, `data-srclang=""`, `data-kind`, `data-rank`, `data-lang="und"`, `data-default`, `data-saved`. No label and no display strings: nothing renders it |
| `.subtitle` (track) | `#subtitle-tracks` — embedded, sidecar, OpenSubtitles, embed externals, AI (and uploads, see below) | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-src`, `data-label`, `data-kind`, `data-badge`, `data-source`, `data-rank`, `data-lang`, `data-lang-name`, `data-lang-flag`, `data-source-badge` (Translated only), `data-forced`, `data-locked` (+ `aria-disabled="true"`), `data-default`, `data-saved`, `data-suggested` (the track the switch would turn on while subtitles are off — `ListItem.Suggested`), `data-offered` (the translation the viewer may start — `ListItem.Offered`, Translated, unlocked, and never the track already playing), plus `aria-disabled="true"` on every chip while the block is muted |
| `.subtitle` (MY) | `#subtitle-tracks`, like every other track. On a page load the dialog's own loop renders them (they are `UserSubtitle` items of the same `GetSubtitles` result); on an async reload `templates/partials/action/user_subtitles.html` renders them into `#my-upload-chips` and the client moves them in | `data-id`, `data-provider="UserSubtitle"`, `data-src`, `data-label`, `data-srclang`, `data-kind="subtitles"`, `data-badge="user"`, `data-rank="0"` (fixed — this view model has no ladder), `data-lang`, `data-lang-name`, `data-lang-flag`, `data-default`, `data-saved`, `data-suggested`, `data-autoselect="true"` when just uploaded, `aria-disabled="true"` while the block is muted |
| `.lang` | `#subtitle-langs` | `data-lang`, `aria-pressed="true\|false"` |

The `none` carrier never takes the active look: `setChipActive` refuses it by `data-id`, the way it
refuses a locked chip. `data-default` still moves onto it — that is the state the switch is read
from — but an invisible chip claiming to be the chosen one, in a group where nothing appears
checked, is not a state worth drawing.

**Classes.** `.track-chip` / `.track-chip-active` on audio and subtitle chips, `.lang-chip` /
`.lang-chip-active` on language chips, `.chip-locked` on a locked AI chip, `.picker-off` on
`#subtitle-langs` and `#subtitle-tracks` while the switch is off (`@apply opacity-50` — dimming
only: every chip stays clickable). They are defined once
in `assets/src/styles/style.css` and are the only thing JS touches: `setChipActive` toggles
`track-chip-active` (plus `aria-checked` and the check icon's `hidden`) and never writes a
Tailwind utility string, never rebuilds a chip's `innerHTML` — which is why `.tr-progress` and
`.tr-spinner` survive every selection.

**Children of a chip**, in order: `svg.chip-check` (hidden unless active) · `span.chip-origin`
(the `EM`/`IN`/`OS`/`MY`/`AI` badge, `title` = the localized origin name) · the label span
(`title` = the full label, visually truncated) · property-tag badges · `· Source` ·
`· from <CODE>` · on `Provider == "Translated"` `span.tr-progress` (text `· 37%`, `title` =
the localized `player.subtitleTranslating` sentence) and `span.tr-spinner`, both `hidden` until a
translation polls and hidden again together on done/error/stop · on a locked chip a padlock `<svg>`
and an `sr-only` explanation. A language chip holds `span.chip-flag`, `span.lang-name`,
`span.lang-count` and `span.lang-dot` (hidden unless the playing track is in that language).

**ARIA.** Audio and subtitle chips are `role="radio"` with `aria-checked`; language chips are
plain buttons with `aria-pressed` — **never** `aria-selected` / `role="tab"`, because there are no
tab panels and the one real choice (which track plays) lives in the radiogroup underneath.

**Server-side `hidden`.** The dialog and the uploads partial both render the bare `hidden`
attribute on every subtitle chip whose `data-lang` differs from the expanded language
(`LangRow.Expanded`), so a page whose picker JS never loaded still shows one coherent language.
The `none` carrier is the one chip the filter never touches: `applyLangFilter` hides it in every
language (it is not a control), and it renders `hidden` from the server. `ExpandedLang` is empty on the async reload of the uploads partial (it has no language row
to consult), so nothing is collapsed for the instant between the swap and the client's `refresh`.

**Refresh order.** `refresh(container)` in `assets/src/js/lib/player/track-picker.js` is
`adoptUploadChips` → `syncLangRow` → `applyLangFilter` → `applyFlagSupport` → `applyOffState`
(→ `syncNow`, a no-op since the "Now:" lines were dropped), and returns the language it settled
on. `adoptUploadChips` leads because every pass after it is scoped to `#subtitle-tracks`, and the
chips of an async reload land outside it; on an ordinary page there is no `#my-upload-chips` and
it is a no-op. `applyOffState`
re-derives the muted state from `data-subtitles-off`, which is how an SSR-rendered "off" picks up
the parts only JS can add (`aria-disabled`). `Player.jsx` calls it after mount, on dialog open, and after the `#my-subtitles` async
swap (upload **and** delete). A plain selection calls `refreshMarks` (`syncLangRow`)
instead: picking a track moves the dot without yanking the viewer out of the language they were
browsing.

**"+N".** `maxVisibleLangChips = 6` (`handlers/action/picker.go`) is mirrored by
`MAX_VISIBLE_LANGS = 6` (`track-picker.js`). `#subtitle-lang-more` is a toggle, not a one-way
expand: collapsed it reads `+N` (N = language chips currently hidden; a language whose count fell
to zero is gone, not collapsed), expanded it reads `×`. `aria-label` stays
`action.stream.moreLanguages` in both states, and the button hides itself at N = 0.

**Origin codes.** `EM` embedded, `IN` in torrent, `OS` OpenSubtitles, `MY` my uploads, `AI`
translation — the same two letters in every locale. Their meaning is carried by the chip's `title`
and by the legend line under the row, both built from `action.stream.origin.{em,in,os,my,ai}`
(`Helper.OriginKey`). **`OS` covers two origins**: a moviehash match on this very file and an
imdb (title) match that may belong to another release. The code and the colour are the same; the
imdb case wears a muted `~` **inside** the badge, and the badge's `title` gains a second sentence,
`action.stream.origin.osImdbHint` ("Matched by title, may be out of sync",
`Helper.OriginHintKey`). Inside the badge because the badge never truncates — the label span is
what does — and because a tilde needs no translation: it replaced a `· hash` / `· imdb` suffix
that printed video-info's raw enum in English in every locale (review M6). `ListItem.Source` is
still carried as `data-source` for telemetry; it is simply not drawn.
Which of the two a track is comes from `moviehash_match` when video-info sends it (since
2026-09-16) and from the `source` enum otherwise (`hashMatched`); `imdbMatched` tests that enum
for `"imdb"` and not for being non-empty, because "the service reported something else" is not
the claim "matched by title" — a placeholder value in the test fixture was enough to put the
warning on seven chips (review I4). The ladder rank is unchanged either way: hash 3, imdb 4. The older `action.stream.badge.*` keys stay in the locale files for
telemetry and back-compat but are no longer rendered — except `action.stream.badge.forced`, because
`forced` is a **property tag**, not an origin: a forced embedded track shows `EM` + `forced`
(`Helper.PropertyTags`). `sdh` is drawn in the uikit and waits for `content-prober` to expose
ffprobe's disposition flags. Since 2026-09-16 an AI chip's `data-source-badge` (the `· from <CODE>`
suffix) can also read `EM`: `pickTranslationSource` accepts an embedded track that carries a
transcoder-session playlist `Src`, where before `embedded` never appeared there. It is now the
*rarest* of the four, not the most common one — the same day's source ruling made a file source
win over the live playlist, so `EM` appears only on files with nothing else to translate.

`data-lang` is the base language tag the chip groups under, `und` when unknown;
`data-lang-name`/`data-lang-flag` carry the display strings so the client can clone a new language
chip out of `#lang-chip-template` without a language table of its own. All three come from
`stremio.NewLangDisplay`.

`data-default`/`data-saved`/`data-suggested` on the uploads list come from
`UserSubtitleTrack.Default`/`.Saved`/`.Suggested`, copied by `Helper.UserSubtitleView` out of the
matching `ListItem` (`us-<uuid>`) of the same `GetSubtitles` call the dialog renders from.
`Suggested` matters most here: an upload is rank 0, so whenever the viewer has subtitles off and
has ever uploaded a file in their language, the track the switch would turn on **is** that
upload — and without the copy the one chip most often suggested was the one chip carrying no
`data-suggested` for the client to find. The view model also carries `SubtitlesOff` (derived from
the same list: the `none` item being default), because while subtitles are off it is the suggested
chip, not the default one, that wears the check and the fill, and this partial has to agree with
the dialog's own track row rather than wait for the client's first `refresh` — the partial has its own view model, so without that
copy the player's audio-switch rule read every upload as "nothing chosen" and could switch away
from a subtitle the viewer had uploaded and picked.

**Only the initial render carries them.** The async reload (`handlers/user_subtitle`, `buildView`)
sends `data-autoselect` and nothing else, and that is deliberate rather than a gap: `markTrack`
returns early when the element already has `data-default="true"`, so a server-rendered default on
the just-uploaded chip would make the client's `activateSubtitle` a no-op — it would never PUT
`ud.SubtitleID`, never clear the previously active chip's mark (two defaults in the DOM at once),
and never mark the upload as playing. The client owns both markers after a reload:
`activateSubtitle` sets and persists the uploaded one, and `syncUploadMarks` in `Player.jsx`
re-derives `data-default` from the live `textTracks` for every other reload.

`syncUploadMarks` reads "the uploads" as `.subtitle[data-provider="UserSubtitle"]` anywhere in
the dialog — containment in `#my-subtitles` stopped being the test when the chips moved into the
radiogroup, and for one instant after a swap a fresh chip is in the wrapper anyway. It falls back
to the `<track default>` attribute when no `textTrack` is showing —
but only while no chip outside the uploads already carries `data-default`. An embedded
(`MediaProbe`) track is driven by hls.js and has no `<track>` element, so "nothing showing" is
also what an embedded track playing looks like; taking a stale `default` as the answer there
marked an upload as well and left two chips with a check. When it does mark a chip it clears
every other `.subtitle` in the dialog, exactly as `markTrack` does, without the PUT: nothing was
chosen here, the DOM is catching up with what is already playing.

`readTracks` (`subtitle-telemetry.js`) matches every subtitle chip via the
`.subtitle[data-provider]` selector, filtering out `data-id="none"`.

## Picker behaviour

One screen, no sub-views (`docs/uikit.html` §19). Audio is a chip row; subtitles are a language
row plus the tracks of the expanded language.

- **The language row filters only.** Pressing a language chip collapses the other languages in the
  track row and changes nothing about playback. The chip of the language currently playing keeps a
  cyan dot, so the selection stays visible while the viewer browses another language — since
  2026-09-15 the row opens on the **preferred** language rather than the playing one, so that dot
  is regularly on a chip whose tracks are collapsed. Past six
  chips the tail goes behind "+N" — except the expanded language, which is never collapsed
  wherever it sorts, and is not counted into "+N": a pressed but invisible filter leaves its
  tracks on screen with no chip pointing at them.
- **Subtitles are switched, not chosen off** (owner, 2026-09-15). A `toggle toggle-soft`
  leads the language row — where the "Off" chip used to be (owner, 2026-09-16) — and there is no
  "Off" chip in either row. Off does **not** empty the block:
  the chips go `.picker-off` (dimmed — `.lang-row` and `#subtitle-tracks`, never the switch itself), every chip keeps its classes — including the active mark on
  the track that comes back, which also carries `aria-checked="true"`, since a chip drawn as
  chosen and announced as unchosen is the worst of both — and the language chip of that track
  keeps its dot. The chips stay
  clickable, and clicking one is "on, with this track": one activation, one `PUT`.
  - Switching off activates the `none` item through the normal path (`persist: true`, so the next
    page load reproduces it), and that activation is what remembers the outgoing track.
  - **The `PUT` is retried once, and only for the latest choice.** `persistTrackChoice`
    (`Player.jsx`) resends `PUT /stream-video/{audio,subtitle}` after `PUT_RETRY_DELAY_MS` (1 s)
    on a rejected fetch or a 5xx, and never on a 4xx — a stale CSRF token or a refused id would be
    refused again. Added 2026-09-16 after two of these came back 503 from the edge on stage
    without reaching the pod and the choice was silently lost; the request is fire-and-forget, so
    nothing noticed. Still fire-and-forget: one retry, no UI, every failure swallowed.
    The body is read eagerly at `markTrack` time, so the retry needs a guard or it becomes a worse
    bug than the one it fixes: click A (503), click B (200) inside the second, and A's retry lands
    last and the next page load restores A. A lost write only ever lost itself; an overwriting one
    corrupts a newer choice. Each call takes the next number for **its kind** (audio and subtitle
    count separately — two independent choices, neither may cancel the other's retry) and the
    retry stands down when it is no longer holding it. `destroyPlayer` bumps a generation counter
    for the same reason: a write queued by a page the viewer has left must not land in the next
    file's session.
  - **The switch has one writer.** "Subtitles are off" means exactly "the `none` item is the
    active one", and `markTrack` (`Player.jsx`) is the only place that writes it: every
    activation moves the switch with it, including the ones the player performs for the viewer —
    a deleted upload landing on `none`, the audio-switch re-pick returning `none`, an upload
    selected while the switch was off. The decision is the pure
    `offStateAfterActivate(prevDefaultId, newId, lastId)` (`track-picker.js`): off iff the new id
    is `none`, and `data-last-subtitle` is written **only** on the way out — the track being
    replaced, or nothing at all when nothing was playing (an older memory is dropped rather than
    kept, since it would name a chip that may be gone). A second writer is exactly how the switch
    and the row drifted apart before.
  - Either way the switch reports itself as a manual choice (`hooks.onSubtitleSelect`, the same
    hook a chip press uses): that is what sets `manualSubtitleRef`, without which the next **audio**
    switch would re-run the ladder and turn subtitles back on over an explicit off. It also stops a
    translation poll the viewer switched away from, and starts one when the track that came back is
    an AI item.
  - Switching on activates, in order: `data-last-subtitle` (this session), the server's
    `data-suggested`, then the ladder rule (`pickDefaultSubtitle`). A candidate counts only while
    it is still in the list and not locked. With nothing activatable at all the switch goes back to
    off rather than claiming a track: nothing is activated and nothing is persisted —
    `toggleDecision` (`track-picker.js`) is that whole rule as a pure function.
  - `ListItem.Suggested` (`data-suggested`) is the server's half: whenever the render's default is
    `none` — the viewer saved it, or the ladder arrived there because the audio is already in their
    language — `GetSubtitles` names the track the switch would turn on. `offSuggestion`'s rungs, in
    order: **1.** the best full human track in the preferred language, **2.** a forced (signs-only)
    track in that language — a real subtitle in the right language beats a full one in a language
    the viewer did not ask for, and this is what the switch offers when the audio is in a third
    language, **3.** an unlocked AI translation (only ever created in the preferred language),
    **4.** the Accept-Language pick, **5.** the best activatable track whatever its language.
    Never the `none` item, never a locked one, and never while subtitles are on: with a track
    playing, `data-default` already answers that question.
    Deliberately *not* routed through `ladderPick`, close as the orders are — every state that
    reaches `offSuggestion` already has `none` marked `Default`, and `ladderPick` answers with the
    first `Default` it finds (index 0) before it can consider anything else, so the call would be
    dead code that silently costs rung 2. The saved-off branch of `applyLadder` still uses
    `ladderPick`, where it runs *before* the defaults are cleared and can therefore answer.
- **A translation is offered, never started for the viewer** (owner, 2026-09-16). Running one
  spends tokens, so nothing but an explicit act begins it:
  - the server never makes the AI item `Default`. Where the ladder's answer was the translation,
    `applyLadder` marks it `Suggested` and the phase-1 selection decides what plays — often
    nothing, i.e. subtitles off.
  - no rule picks it up either: `pickDefaultSubtitle` (the audio-switch rule) skips
    `provider === "Translated"`, `toggleDecision` accepts one only through `data-last-subtitle`
    (already run this session, therefore cached and free), and `offSuggestion` lost its AI rung.
  - the 5-second engagement-gate auto-start in `Player.jsx` is **gone**. A run starts on a click
    of the chip, on the switch restoring `data-last-subtitle`, or on the **mount-time restore of a
    saved translation**: a translation is not a `<track>` in the page (`markPreload` skips it), so
    unlike every other saved choice it cannot resume by itself, and the deleted gate was what used
    to start it. `restoreSavedTranslation` (`track-picker.js`) answers whether to — saved **and**
    playing **and** unlocked — and `Player.jsx` runs the click path once with `persist: false`.
    That run is the viewer's own and already cached, which is what makes it the one automatic
    start left.
  - an offer taken is an offer spent: `setChipActive` drops `data-offered` and `.chip-offered`
    when the chip is activated, so the accent and the hint cannot resurface after a run.
  - **`Offered` is a different field from `Suggested`** (owner review, 2026-09-16). They answer
    two questions — "what can the viewer start here" and "what would the switch bring back" — and
    one list usually needs both, on two different chips. `data-offered` marks the translation the
    ladder would have chosen, and never the one already playing — a translation the viewer saved
    comes back as `Default`, and inviting them to start what is on screen is nonsense;
    `data-suggested` stays the switch's restore candidate, computed by `offSuggestion` (which
    still runs when an offer exists — the two fields do not share a slot), and is never a
    translation. A **locked** item is never `Offered`: a free
    viewer cannot run it, so its chip keeps the old name-plus-lock shape and the upsell CTA.
  - the chip says which it is: offered, it reads as a verb (`✦` + the `AI` badge +
    `action.stream.translate.action`, "Translate to Portuguese") and wears `.chip-offered`, an
    accent outline instead of the cyan fill that means "this is playing"; otherwise it reads as
    before (`Portuguese · from IN` + progress). Two spans, `.ai-action` and `.ai-label`, flipped
    by `setChipActive` — no `innerHTML`, so `.tr-progress` survives. The verb spans are rendered
    only for an offered item, so nothing hidden lingers on a locked chip, and `setChipActive`
    flips nothing on a chip that has no verb (a locked one, or one whose offer was taken: without
    that test the clearing pass hid the label and left an empty button — found in review,
    2026-09-16).
    **A finished translation reads as the verb again after a reload.** The client drops
    `data-offered` when the run starts, but the ladder does not know the run happened, so the next
    render offers it again — "Translate to Portuguese" on a file that is already translated.
    Harmless, because re-selecting it costs nothing (the VTT is in S3), but it is why the chip's
    wording cannot be read as "this has never been translated".
  - `#subtitle-hint` under the track row (`action.stream.translate.hint`) is the **offer in
    words** — "no subtitles in your language yet, turn on the AI translation" — and says nothing
    about the switch. Three conditions, all of them needed for the sentence to be true: a chip is
    `Offered`, it is not the track already playing, and **its language holds nothing else the
    viewer could turn on** (one forced sidecar in that language and "no subtitles yet" is simply
    false). The server renders those three; the client reads the same three off the chips
    (`offerNeedsHint`, `track-picker.js`) and re-applies them in `refresh` and `refreshMarks` —
    which every activation ends in, so an offer taken stops being explained at once.
  - both sentences are rendered **server-side** with the language name substituted: Go templates
    take `{{.Param}}`, the client's `tf` takes `%v`, and a chip Go renders once has no reason to
    learn the client's formatter. **The name inside these two sentences is localized into the UI
    language** (`langDisplayIn $.Lang <tag>` → `.Localized`, `golang.org/x/text/language/display`
    — "немецкий" for German on a Russian UI, falling back to the English name when the UI locale
    has no CLDR entry or the tag itself is unlisted), added 2026-09-16 (item 9 of the track-picker
    work) so a Russian viewer reads "Перевести на немецкий", not "Перевести на Russian". Chips
    keep the plain `langDisplay` → `.Name`, the same **English** name every other chip shows
    (Discover-style, deliberately not localized) — only the two sentences changed.
- **The active chip is a check icon plus a cyan fill** (`track-chip-active`), never an underline —
  underline vanished on touch hover and did not read under colour blindness. Exactly one chip is
  active per group, and a locked chip can never take the mark.
- **No "Now:" summary**: the active chip (check + fill) is the only indicator of what is playing;
  the heading-line summary from the first mockup was dropped as noise (2026-09-15). `#audio-now` /
  `#subtitle-now` no longer exist; `syncNow` no-ops when they are absent.
- **Uploads are inline, and split across the radiogroup boundary** (a11y, 2026-09-16). The MY
  chips are radios and sit in `#subtitle-tracks` with every other track, grouped by language. The
  dashed `+ My Subtitles` disclosure (`action.stream.mySubtitles`, the same key the old tab used)
  and `#my-uploads-panel` are not radios and sit in `#my-subtitles`, a sibling block **after** the
  row: the panel holds a heading, a close control, the upload form and one delete form per file,
  and a `role="radiogroup"` contains radios and nothing else.
  Each delete form still posts to the unchanged `POST /user-subtitle/delete/:id` with
  `data-async-target="#my-subtitles"` — one swap target, as before.
  Deletion is deliberately not on the chip — a chip is a radio button, and an accidental tap must
  not destroy a file. After the swap `Player.jsx` re-runs `refresh`, so the chip row, the language
  counts and the expanded language all follow; the panel re-opens from `data-upload-open` on
  `#my-subtitles` (the wrapper survives the swap, the toggle and panel inside it do not).
  - **Who renders the chips depends on which render it is** (`UserSubtitleView.RenderChips`). On a
    page load the dialog's own `$subs` loop renders every upload into the row — they are
    `UserSubtitle` items of the same `GetSubtitles` result — and the partial emits none, so a page
    whose JS never ran is already correct. On the async reload nothing re-runs that loop (the
    partial *is* the response), so it renders them into `#my-upload-chips` and
    `adoptUploadChips` (`track-picker.js`) moves them into the radiogroup.
  - **The marker is what makes a delete work.** `adoptUploadChips` replaces *every* MY chip in the
    row with what came back, so an upload's chip is the fresh element (de-dup by `data-id` falls
    out of that) and a deleted one leaves the row instead of being orphaned in markup nothing
    re-renders. That is only safe because `#my-upload-chips` says "this response is the complete
    current list": an empty wrapper is a delete, an absent wrapper is an ordinary page. The
    wrapper is removed once drained, so the next `refresh` does not read a consumed answer as
    "this viewer has no uploads".
  - **"I do not know" must not render as "there are none."** `buildView` takes `listOK` and sets
    `RenderChips` from it, so a failed `List` — which returns a nil slice — renders the panel and
    the error and **no marker**. Without that gate a transient DB blip during an upload would emit
    an empty authoritative list: every MY chip out of the row, the playing upload's `<track>`
    dropped by `dropDeletedTracks`, playback on Off, from an error the UI only shows as a toast.
  - **The adopted block keeps the place the server gave it.** `adoptUploadChips` re-inserts at the
    position of the outgoing MY block (the first element after it, captured before anything is
    removed), not at the tail. Uploads are rank 0 and the dialog renders them ahead of the AI
    item; appending moved them past it on every upload and delete, and the next page load moved
    them back. On a viewer's very first upload there is no block to restore and the chip goes to
    the end, which the next render puts right.
  The panel closes from the chip again **or** from the "×" in its heading line
  (`#my-uploads-close`, owner 2026-09-15): the way back should not depend on remembering which
  chip opened it. Both controls go through `setUploadPanel`, so the state left behind is the same
  one either way.
- **Deleting the upload that is playing lands on Off.** The swap replaces `#my-subtitles`, so the
  chip goes, but the `<track>` lives in `<video>` and would keep the deleted file's subtitles on
  screen with nothing marked. `dropDeletedTracks` (`subtitle-track-reload.js`)
  removes every `<track>` no chip claims any more — the test is the id against **all** chips in
  the dialog, since the preloaded OpenSubtitles and sidecar tracks are `<track>` elements too —
  and reports whether one of them was showing; when it was, the player activates `none` with
  `persist: false`. The viewer chose a deletion, not a track, so nothing is PUT.
- **Every player dialog closes on a click outside its box** (owner, 2026-09-15). `#subtitles` and
  `#embed` each end with DaisyUI's backdrop — `<form method="dialog" class="modal-backdrop">` with
  a submit button, as the dialog's **last** child. The form stretches across the same grid cell as
  `.modal-box` at `z-index: -1`, so markup order is what keeps the box's own controls on top, and
  `method="dialog"` is what closes the dialog (a `type="button"` there would submit nothing and the
  click would do nothing). The dialog is modal, so the click never reaches the player underneath.
  The button's label is literal and untranslated, like every other modal here: it is visually
  hidden but focusable, and a translated one makes a screen reader announce "Close" twice.
- **Flags fall back.** `supportsFlagEmoji()` (`lib/discover/lang.js`) hides every `.chip-flag`
  where the platform draws bare letter pairs (Windows outside Firefox); the language names stay.
- **Language-row order** (owner, 2026-09-15): the viewer's **preferred** language first — including
  a group whose only track is the AI translation — then the language of the track playing, then the
  language of the track the switch would turn on (`Suggested`, which exists only while subtitles
  are off, so it never competes with the playing one), then by track count, then the order
  `GetSubtitles` produced. Expanded and first are always the same chip: `Overflow` is assigned by
  index, so a language expanded from further down the row would be pressed and collapsed at once. The row opens on that first chip
  (`LangRow.Expanded`), so a page opens in the viewer's own language whatever is playing; the
  playing track keeps the cyan dot on its chip wherever it sorts, and may sit hidden under the
  filter, which is accepted. A preferred language with no tracks at all changes nothing — the
  playing language leads again, as before.
  `SubtitleLangGroups` (Go) and `groupByLang` (JS) must stay identical — the client recomputes the
  row after an upload changes the counts — and the same fixture is in both test suites. On the
  client the viewer's own browsing choice still comes first: `expandedLangFor` keeps the pressed
  language whenever it still has tracks, and falls back to this order otherwise (on a first open
  the pressed chip *is* the server's `Expanded`). The suggested-language rule needs no clause of
  its own there: `readChips` already reads the muted choice — `data-last-subtitle` this session,
  else the `data-suggested` chip — as the active one, so it sorts into the same slot. The client
  never reorders the row itself; `syncLangRow` updates the chips in place and the order stays the
  server's. There is deliberately **no** alphabetical
  tie-break: it would reshuffle every equal-count group on the first refresh after an upload.
- **Without JS** (picker JS failed, player loaded): every chip renders, the expanded language's
  tracks are visible, the active track carries its check and fill from SSR and the counts are
  right, and the muted state is drawn (the server renders `data-subtitles-off`, the toggle's
  `checked` and `.picker-off`). Missing are the filter (the language row is inert), the dot, the
  switch itself (a checkbox nothing listens to) and the `aria-disabled` markers, which
  `applyOffState` adds on the first `refresh`.
  Switching tracks needed JS before the redesign too — the handlers were always client-side.

**A11y wart — resolved (2026-09-16, ruling R8).** `#my-uploads-toggle` and `#my-uploads-panel`
used to render *inside* `#subtitle-tracks[role="radiogroup"]`, because the uploads partial is a
single async swap target that had to emit both the MY chips and the panel, and its `#my-subtitles`
wrapper (`display:contents`) sat inside the row — so the radiogroup contained a disclosure button
and, when open, two forms. `#my-subtitles` is now a sibling **after** the row and keeps the panel;
the chips stay in the radiogroup, rendered by the dialog's own loop on a page load and moved in by
`adoptUploadChips` after a swap (see "Uploads are inline" above).

What it cost, since the estimate was wrong: not markup plus a re-scope of `readChips`, but a
render-mode field (`UserSubtitleView.RenderChips`) so one upload is not rendered twice, a marker
element (`#my-upload-chips`) so a delete is distinguishable from a page load, and a re-scope of
`syncUploadMarks` (`Player.jsx`) from "inside `#my-subtitles`" to
`data-provider="UserSubtitle"`. `readChips` itself needed no change — it was already scoped to
`#subtitle-tracks`, which is where the chips ended up. The two multi-target alternatives were
rejected: `loadAsyncView` swaps exactly one element, and the `data-async-update-*` slot mechanism
that could carry a second fragment is a **layout** facility whose key→action map lives in
`app/layout.js` — adding a picker-specific slot there would put a player detail in the global
navigation updater.

Server side: `handlers/action/picker.go` (`SubtitleLangGroups`, `OriginCode`, `OriginCodeForBadge`,
`OriginKey`, `PropertyTags`, `AudioSuffix`) and `services/stremio/lang_display.go`
(`langDisplay`/`Name` for chips, `langDisplayIn`/`Localized` for the two sentences). Client side:
`assets/src/js/lib/player/track-picker.js`, wired in `Player.jsx`.

## Known limitations

- **Job cache key ignores a preferred-language change.** The 10-minute streaming-job cache key
  (`jobs/scripts/action.go`, `Action`) is built from resource/item/action/`c.ApiClaims.Role`/
  settings/audio+subtitle choice/`c.Lang`/session. Tier **is** in it — `Role` is the tier name
  (`services/api/api.go`: `cl.Role = uc.Context.Tier.Name`), so a viewer who upgrades gets a
  different key and the lock lifts at once. What is not in it is
  `stremio_settings.preferred_language`: a viewer who changes that in their profile may keep
  seeing the old `SubtitleOpts` (ladder run on the old language) until the current 10-minute
  bucket rolls over. Parked as a follow-up, not fixed in this task.
- **Embedded-track translations follow the viewer's transcode.** Since the 2026-09-16 source
  ruling this is the *last* resort — it is reached only when the file has no upload, no sidecar
  and no OpenSubtitles track to translate — but where it is reached the source is the live
  subtitle playlist of the current transcoder session, so the translation runs at transcode
  speed and stops when the viewer leaves (`--live-idle` on the service) or seeks far (a new run
  with a non-zero offset). A seeked session never produces a cached final artifact; the partial
  progress is kept 24 h and reused by cue identity. Native MP4 without a transcoder session
  still has no embedded source.
- **After a seek, translated cues can sit up to one GOP off.** Measured on the stand
  (2026-09-16): the same line lands 1.66 s apart in two runs of one file (seek 600 vs 570) —
  the transcoder starts each run at the keyframe before the quantized offset, and the player's
  own timeline carries the same per-run shift, so embedded tracks stay in sync while every
  side-loaded track (OpenSubtitles, uploads, AI) is off by that shift. The service dedups cues
  by text within a 3 s window so a seek does not duplicate lines; the timing it keeps is the
  first run's. A transcoder-side fix (report the real run start) would remove it for all tracks.
- **Each reload refetches the whole partial VTT.** The `rev` reload swaps the `<track>` `src` and
  lets the browser reparse from scratch — no incremental cue-append; the file is just short early
  on and grows with each revision. The 15 s throttle bounds the cost (and the blank-cue window) but
  does not remove it: the subtitle text catches up in steps while the percentage moves smoothly.
- **A translation is reported once per item per page load.** `translationAction(track, status)`
  (`subtitle-rules.js`) reads a per-item status map (`'running'`/`'done'`/`'stopped'`) and answers `'start'`,
  `'resume'` or `'none'`. `'start'` emits `subtitle-translate-start`; `'resume'` polls again
  **silently** after the viewer selected another track mid-run and came back (same translation, so
  no second start event — `done` still fires once, with the wall time since the *first* start);
  `'none'` covers a finished item, a locked one and anything that is not an AI track, so a warm
  cache cannot fire a second start/done pair when the same item is activated again (the
  mount-time restore of a saved translation, say). Re-selecting the
  item whose poll is running right now is also a no-op (`pollingIdRef` in `Player.jsx`) rather than
  a self-inflicted stop.
- **After an error, the item stays `'running'`.** Re-selecting it resumes (a silent retry) rather
  than reporting a fresh start; each attempt can still emit its own `subtitle-translate-error`.
  The one exception is `'stopped'`: the service ended that run for good, so `fail` marks the item
  `'stopped'` and `translationAction` answers `'none'` — re-selecting the chip neither polls nor
  reports, which is what its own "reload to retry" title already promised. A reload is a fresh
  status map, and that is the retry.
- **Polling gives up after 15 minutes without a sign of life** (`POLL_TIMEOUT_MS`,
  `subtitle-progress.js`) with `subtitle-translate-error {code:'timeout'}`. What the 15 minutes are
  measured from depends on the source: for a batch source the cap is **absolute** (the deadline is
  set once, at the start of the run — its end is minutes away, not film-length), while for a live
  source (`X-Subtitle-Live`) it is an **inactivity** cap — the deadline moves forward on every tick
  that reports a *new* translated cue. A live run therefore lasts as long as it keeps producing,
  i.e. the length of the transcode, but a wedged live job that keeps answering the same count still
  times out after 15 minutes: answering is not progress. Time the poll spends suspended (paused or
  hidden, see *Client behaviour*) is not counted at all.
- **A `rev` can go backwards into a browser cache after a seek.** A session seek starts a new run
  at a non-zero offset, so `done` drops; `withRev(src, done)` then reuses a `rev` the browser
  already has, and the `<track>` GET carries no `no-store`, so the viewer can get the previous
  run's shorter VTT back.
- **A finished translation reads as the verb again when it is deselected.** The `.ai-action` span
  is rendered `{{ if .Offered }}` and stays in the DOM after the offer is spent, so
  `setChipActive(el, false)` (`track-picker.js`) re-shows "Translate to <language>" for a track
  that is already translated and cached. Harmless; the guard's comment claims to cover this case
  and only covers the locked one.
- **`markPreload` follows the browser's languages, not the preferred one.** It reads
  `ud.AcceptLangTags[0]`, not `opts.PreferredLang`, so a signed-in viewer whose
  `stremio_settings.preferred_language` differs from their browser gets no preloaded `<track>` in
  the language the ladder just ran on — and the native iOS menu then offers the wrong two.
- **A live run is suspended, not finished, when the film ends.** The media spec fires `pause`
  before `ended`, so a viewer who watches to the end of a file whose translation is still behind
  the transcode leaves the poll asleep on the last count: the chip keeps its spinner, no
  `subtitle-translate-done` and no `-error` is emitted, and the remaining cues are never fetched.
  Consistent with "suspending is not stopping", but it means the `-done` rate for
  `source=embedded` is lower than the funnel would otherwise suggest — and the cached final
  artifact for a contiguous run from 0 may be lost exactly for the viewers who watched the whole
  film. Watch it before reading the funnel as a regression.

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
  covers the upload partial, including the `RenderChips` split (initial render: panel only; async
  reload: chips wrapped in `#my-upload-chips`).
  `TestStreamVideoRendersUploadChipsInsideTheTrackRow` renders the real partial inside the dialog
  and balances `<div>` tags to prove the chip is *inside* the radiogroup and the disclosure, the
  panel and every form are *outside* it — a substring order check cannot tell nesting from
  adjacency, which is how the wart survived the first review.
- JS: `npm test` (`node --test`). **`Player.jsx` is covered now** —
  `assets/src/js/test/register.mjs` (passed as `--import` by the `test` script) installs the
  module hooks in `assets/src/js/test/jsx-hooks.mjs`, which transpile `.jsx` with `@babel/core`
  through the repo's own `babel.config.json`, resolve the extensionless relative imports webpack
  resolves, and answer the `.css` and `locales/*.json?prefix=` imports with empty modules.
  `jsdom` (pinned devDependency) supplies the DOM.
  `Player.wiring.test.js` drives the wiring against
  `assets/src/js/lib/player/__fixtures__/subtitles-dialog.html` — the dialog as
  `services/template/subtitles_dialog_fixture_test.go` renders it, committed so `npm test` needs
  no Go toolchain — along with `user-subtitles-async.html` and `user-subtitles-async-empty.html`,
  the uploads partial rendered with `RenderChips: true` for the upload and the delete case, which
  is what `asyncSwap()` replays. **Regenerate all three** whenever the picker markup changes:

  ```
  UPDATE_FIXTURES=1 go test \
    -ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' \
    ./services/template/ -run TestSubtitlesDialogFixture
  ```

  The `-ldflags` are not optional: without them the test binary panics at init on the
  abuse-store/torrent-store proto conflict, exactly as `go test ./...` does. That Go test fails
  with this command in the message when the fixtures drift, so a wiring test cannot go on passing
  against markup the server stopped producing.
  Covered: a chip click (mark, `<track>` creation, PUT body, telemetry), a click landing on an
  inner span, the switch off/on/off with one PUT each, a chip click while off, the language filter,
  the "+N" toggle, the uploads swap (adoption, autoselect, de-dup, panel state), a delete of the
  playing upload landing on Off without a PUT, `syncUploadMarks` including the stale-`default`
  guard, the PUT retry, the offered-AI chip's verb/label flip, the locked chip's CTA, and — through
  a real `initPlayer` mount — a click starting a translation run, a saved translation restoring
  once without persisting, and nothing starting a run on its own.
  Still by hand: fullscreen, and anything about actual playback.
  The plain-JS modules keep their own suites: `subtitle-rules.test.js`
  (`pickDefaultSubtitle`, `baseLang`, `translationAction`, `hasSavedDefault`),
  `subtitle-progress.test.js` (`parseProgress`, `withRev`, `pollProgress`),
  `subtitle-telemetry.test.js` (`readAllTracks`, `readTracks`, `selectEventData`,
  `resolveSubtitleLevel`), `subtitle-track-reload.test.js` (`reloadSubtitleTrack`: listener
  lifetime, latest-snapshot restore, the did-it-reload return value), and `track-picker.test.js`
  (`adoptUploadChips`: no marker means no move, an upload replaces the chip of its id, a delete —
  including of the last file — takes the chip out of the row).
