# Partial archive downloads (select mode)

Directory listings let the user archive a subset of files/folders instead of
the whole directory. JS-only progressive enhancement: without JS the TAR/ZIP
buttons keep their whole-directory behavior.

## UX

- A "Select files" toggle (`resource.select` / `resource.selectCancel` i18n
  keys) sits next to the TAR/ZIP split button in the file browser card
  (`templates/partials/list.html`). It is SSR-rendered hidden and unhidden by
  `assets/src/js/app/resource/select.js`.
- In select mode every row (files and directories) shows a checkbox
  (`list_select_box` template define; `aria-label` = row name). A checked
  directory covers its whole subtree — no need to expand it.
- The size shown on the TAR/ZIP buttons switches from the SSR
  whole-directory size to the client-side sum of the checked rows'
  `data-size` values (formatted by a JS clone of go-humanize `Bytes`, which
  is what the template's `bitsForHumans` maps to — base 1000; the local Go
  `helpers.Bytes` is base 1024 and NOT the one this label uses). Nested
  selections are pruned before summing (folder + file inside it counts
  once). Like the original label, this is the content sum; archive header
  overhead is negligible and not included.
- Empty selection (or leaving select mode) falls back to whole-directory
  download.

## State model (`resource/select.js`)

Selection lives in a module-level Map keyed by `resourceID:dirPath`, so it
survives async `#list` reloads (pagination re-renders the card) but resets
when the user navigates to another directory or reloads the page. The `av()`
init re-runs on every `#list` render and re-applies checkbox state, button
labels, and the hidden inputs. Rows checked on other pagination pages stay
selected even while not rendered. Ticks past the server-side bounds (1024
entries / 6000 encoded bytes, `MAX_PATHS`/`MAX_ENCODED_LEN`) are refused at
click time.

## Wire format

- The checked paths are pruned (entries covered by a checked ancestor folder
  dropped), sorted, and written as repeated hidden `<input name="paths">`
  elements into **both** archive forms (TAR and ZIP are separate forms
  because async.js posts `new FormData(form)` without the submitter value).
  Values travel verbatim — torrent path components may legally contain
  whitespace or newlines, so there is no joining/splitting anywhere.
- `/download-dir` (`handlers/action/handler.go`) reads them with
  `GetPostFormArray`, enforces the count and encoded-length caps, sorts
  (canonical set order → stable job cache key) and passes them through the
  job chain (`jobs.Action` → `scripts.Action` → `download()`).
- The slow-download "Continue at slow speed" resubmit form re-posts the
  `archive-format` and `paths` fields (via `SlowDownloadData`), so the slow
  re-run keeps the format and selection instead of falling back to a
  whole-directory zip.
- `services/api` adds each path as a repeated `paths=` query value on the
  rest-api export call. rest-api (`DownloadURLBuilder.getSelectedPaths`)
  validates every path against the torrent manifest and the exported
  directory — error texts carry the `errorHandler` substrings so client
  mistakes map to 400/404, not 500 — and appends the sorted values to the
  signed download URL (`~arch`). The encoded-length cap exists because edge
  proxies reject request lines past ~8k; it, not the 1024 count, is the
  effective selection bound.
- torrent-http-proxy forwards the query untouched; torrent-archiver
  normalizes/sorts the values (NUL bytes rejected), computes the filtered
  file list **once** per request (shared by Size and Write — resume Range
  requests stay cheap), matches entries exactly or by folder-prefix with a
  path-boundary check, includes the selection in the archive `ETag`, and
  returns 404 on an empty intersection instead of an empty archive.

Paths are torrent-rooted (`/TorrentName/sub/file.mkv`, the listing's
`PathStr`); the archiver trims the leading slash to match metainfo paths.

## Known limitation

`download()` still gates readiness on the whole-directory warmup
(`torrent_client_stat` of the directory + `Source.Size`): with a selection
that excludes the directory's first file, warmup waits on head pieces the
archive won't stream. Harmless for availability (the seeder session warms
either way) but the "download ready" signal can be pessimistic on cold
swarms. Follow-up if it bites: skip or re-target warmup when a selection is
present.
