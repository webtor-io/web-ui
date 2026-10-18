# File info

"File info" (ru "О файле") on the selected file's card shows what a video or
audio file holds **before** it is downloaded: its audio tracks (language,
title, codec, channel layout, which one is the default), its embedded
subtitles (language, title, format, picture-based or text, default / signs
only / SDH) and its video (codec, size, HDR10 or HLG, bitrate) and duration.

Why: a hard-of-hearing viewer who downloads to watch in VLC could not tell
beforehand whether English audio is the default and whether English soft
subtitles are inside. The player's picker does not answer that: its
"default" is the viewer's own pick or language match, and it hides picture
subtitles the browser cannot draw.

Free for everyone; anonymous starts pass Turnstile like every job start
(docs/turnstile_actions.md).

## Flow

- Button: `partials/file.html`, video and audio only, built by
  `handlers/resource` `MakeFileInfo` (`partials/button.html`, action
  `file-info`). An icon on a phone beside Watch and Download. The deep link
  `#action=file-info` and the Umami click event `file-info` come from the
  form's class and `data-umami-event` with no code of their own.
- `POST /file-info` → `h.post(c, "file-info")`: CSRF, Turnstile for
  anonymous starts, the job in the `file-info` queue (`webui_jobs_total`
  label). The client lists the route in `lib/turnstileAction.js` and
  `lib/actionBusy.js`; `handlers/action` `TestJobStartRoutesRequireTurnstile`
  fails for a POST route left out of the server's list.
- The job (`jobs/scripts/file_info.go` `fileInfo`):
  `job.retrievingDownloadLink` → rest-api export (30 s; a stoplisted file is
  refused here, `error.forbidden`; rest-api's 404 is no error and no link →
  `error.not_found`; a format other than video/audio → `error.probe_failed`)
  → `job.probingMedia` → `GetMediaProbe(<download URL>~cp)` with a 60 s
  deadline → `action/file_info` rendered into `#log-<ItemID>`.
- **No warm-up.** The probe runs as `services/thumbnail` runs its own: a
  probe content-prober already holds (7 days, shared with Watch and the
  thumbnail, key infohash+path) costs nothing, and a cold torrent pays only
  what ffprobe reads. This relies on content-prober 030c8ee ("do not cache a
  probe the caller abandoned"): before it, a probe this job gave up on was
  cached as the file's failure for an hour and broke the next Watch of a
  transcoded file. Deploy content-prober first.
- The card replaces whatever is in the file's log, a playing player included,
  and Watch replaces the card (MVP; a `<dialog>` of its own is the next step,
  as Download has).

## What the card says (`buildFileInfo`, a pure function)

- **Default audio**: the first audio track with `disposition.default=1`, else
  the first audio track — what a player with no preferred language starts
  with. Matroska's FlagDefault defaults to 1, so "every track default" is
  common (5 of 51 multi-audio files in a live sample, 2026-10-18): only the
  first counts. The card's hint says a player set to a language may pick
  another.
- **Default subtitle**: the first flagged, no fallback (an unflagged subtitle
  is not shown by default).
- **Forced**: `disposition.forced`, or "forced" as a word in the title (the
  player's `forcedTitleRe`). **SDH**: `disposition.hearing_impaired`, or
  "SDH"/"CC" as a word in the title.
- **Picture-based** subtitles (PGS, VobSub, DVB, XSUB) are listed and marked
  "external player only".
- **Language**: the tag canonicalised (`eng` → `en`) and named in the page's
  language (`langDisplayIn`); an untagged or `und` track reads "Language not
  specified" — never guessed.
- **Video**: cover art (`attached_pic`) skipped; HDR10 for `smpte2084`, HLG for
  `arib-std-b67` (Dolby Vision needs `side_data_list`, not read yet); bitrate
  from the stream (or mkvmerge's BPS tags), in the status's megabit.
- Not shown: chapters, container tags (free text, no use here), OpenSubtitles
  or sidecar subtitles, a "for your language" verdict (44% of a live sample had
  an untagged audio track).

The disposition and `color_transfer` fields come only through the `~cp` path
(raw ffprobe JSON). The transcoder's `media_probe` export goes through
content-prober's gRPC proto and lacks them, so it is not used as a fallback.

## Errors (`probeError`)

| Probe answer | Shown |
|---|---|
| our 60 s deadline (the swarm did not deliver the head) | the no-peers "timeout" card with "Try again" (`actionEndpoint` maps `file-info`) |
| content-prober's 500 `probing failed: …` (ffprobe failed on the file) | `error.probe_failed` |
| the same with `context canceled` / `deadline exceeded` (ffprobe was cut off) | `error.upstream_unavailable` |
| thp's 502 (prober down), thp's empty 500, a transport error | `error.upstream_unavailable` |

How often the deadline hits: Loki,
`{app="web-ui"} |= "no peers / warmup deadline" |= "action=file-info"`; other
failures are `webui_jobs_total{job="file-info",outcome="error"}`. If timeouts
pass ~5% of starts in a week, add the stream's quick warm-up back before the
probe.

## Privacy and policy

- The job log replaying the card is public (`Access-Control-Allow-Origin: *`):
  the card holds no URL. Track titles are escaped (`html/template`).
- Nothing is stored or aggregated: no table, no index, no Discover filter
  (the no-content-indexing policy). A repeat press in the same session within
  the job key's 10-minute bucket replays the finished job.

## Later

`<dialog>` instead of the shared log; a warm-up fallback if timeouts are
common; Dolby Vision; file-info wording for the no-peers card; the player's
forced/SDH detection moved to `disposition` (`handlers/action/helper.go`,
`picker.go` still guess from the title).
