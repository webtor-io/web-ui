# Uploads

An account's own files, sent from the browser straight to a seeder through
torrent-http-proxy and kept there as one directory per upload, on the node
that took it (its home node). Only its owner sees it. It is temporary — the
cleaner may evict it when its files have not been opened for a while and the
space is needed — unless it is saved to Vault.

web-ui's part: the IDs, the on/off gate, write tokens and the per-user quota,
the upload's page, "My uploads" in the library, deleting, and erasing what
web-ui derived from an upload once it is gone. The seeder enforces the write
limit and keeps tombstones, its cleaner evicts; rest-api decides who may read
an upload.
Code: `services/uploads`, `handlers/upload`, `assets/src/js/lib/drop.js`,
`upload.js`, `uploadFile.js`.

## When uploads are on

`--upload-url` / `UPLOAD_URL` is the torrent-http-proxy address the **browser**
sends files to. Uploads are on when all of these hold (`uploads.Enabled`), and
the process says at start which (`uploads: on`, or `uploads: off, <why>`):

- `UPLOAD_URL` is set;
- `WEBTOR_API_SECRET` is set — without it web-ui signs no write token;
- rest-api is not reached through RapidAPI — there rest-api sees one account
  for every user and cannot keep an upload to its owner.

With uploads off `uploads.New` returns nil and nothing of them is served:
`/upload/*` is not mounted, the drop zone takes `.torrent` files only (no
`multiple`, no sign-in card), the tool page `/private-cloud-media-server` is
dropped from routes, sitemap and footer (`DropUploadPages`,
`docs/tool_pages.md`), and the home badge says "no size limit". "My uploads"
stays in the library menu while it has rows, to list and open them; its bulk
delete is not offered.

web-ui's own calls to the seeder (delete, seal) do not go to `UPLOAD_URL`, which
the server side may not reach: they take the way of web-ui's other
server-side requests to torrent-http-proxy (`api.ProxyURL`, the internal proxy
with `--use-internal-torrent-http-proxy`), with a service token of web-ui's own
bound to the upload.

## IDs

40 lower-case hex characters: the prefix `9c5db5752b7c` (`models.UploadPrefix`),
one hex digit of kind, then 27 random hex (`uploads.NewID`). The prefix tells an
upload from an infohash without asking anyone:

- `uploads.HomeHosted(id)` — any kind: the content lives on its home node, not
  in a swarm. No folder archive, the Stremio library finds a file by its path
  (`docs/stremio.md`).
- `uploads.Owned(id)` — kind 0, a user's upload: one owner, who alone writes
  and reads it. Private by its ID, whatever rest-api's `private` says
  (`docs/share_previews.md`).

The minute caches of rest-api answers (`GetResourceCached`,
`ListResourceContentCached`) skip the whole prefix: an upload's files change,
and a deleted upload must stop answering at once. The predicates are strict:
an ID in upper case is none of them.

## The write token: `POST /upload/token`

Signed-in only (`auth.HasAuth`: 401 to a script). JSON body, exactly one of:

- `{"name": "<root>", "want": <bytes>}` — a new upload; `want` ≥ 1. Its
  `torrent_resource` and library rows are written in the transaction of its
  reserve (`models.CreateUpload`), so a refusal leaves none.
- `{"id": "<upload>", "want": <bytes>}` — more files into an upload, or a
  renewal: `want` 0 renews at what the upload already holds.

`200 {"id", "url", "token", "apiKey", "cap", "used"}`: `url` is
`UPLOAD_URL/<id>`; the token is the viewer's api claims bound to the ID with
`up` (`new` or `add`) and `lim`, the write limit, for 15 minutes
(`uploads.TokenTTL`). Rules and the internal mark of the viewer's claims are
dropped.

| Status | When |
|---|---|
| 400 | the body is not one of the two shapes, or `want` is negative |
| 401 | not signed in |
| 404 | the ID is not a kind-0 upload, or rest-api refuses it to this viewer (not theirs) |
| 409 `{"code":"frozen"}` | the upload is saved to Vault: the seeder takes no more writes |
| 410 | rest-api does not know the upload (gone) |
| 413 `{"code":"quota","used","cap"}` | no room left in the account's quota |
| 503 | rest-api or the quota check did not answer (10 s for rest-api, 5 s for the check) |
| 500 | no claims, a database failure, or the token could not be signed |

## Quota

10 GiB on the free tier, 50 GiB on a paid one (`uploads.Limit`, by the tier in
claims). Table `upload_reserve` (migration 77): one row per account and
upload — `reserved_bytes`, `write_until` (the token's end), `gone_at`. A row is
written by the first token and removed only with the account; it is in the
data export (`docs/data_export.md`).

- **Used** (`models.UploadUsed`): the sum of the rows that count — a row weighs
  its `reserved_bytes` while its token is alive (plus 5 minutes,
  `uploads.WriteGrace`) or while it is not marked gone. An upload pledged in
  Vault does not count: VP pay for it.
- **Reserve** (`uploads.Reserve`, under the account's advisory lock
  `QuotaLock`): the upload's own bytes — the seeder's, as rest-api reports
  them, or its live reserve if larger, so a renewal or a second tab never
  shrinks it — plus `want`, as far as the cap minus the other uploads allows.
  Nothing left → 413.
- **Live check** (`Service.Quota` with `check`, on every token request): each
  counted row past its token and not yet gone is asked about at rest-api, as
  the viewer, at most 4 at once, 3 s each, in random order. The seeder's bytes
  are written down; a 404, or 403 `no_record` for the owner, marks the row gone
  and forgets the upload (below, `certain` false). Any other answer writes
  nothing: the row keeps its last known reserve.

The profile shows used of cap (`profile/uploads_quota.html`, no live check);
the drop zone's badge says the cap, or the free tier's upsell. On the tool
page about uploads (`Tool.Uploads`) it is the own-files half alone, and
there is no badge without claims; that page also has no magnet field, demo
or torrent blocks around the zone (`docs/tool_pages.md`).

## The browser's way

`lib/drop.js` serves the drop zone of the home page and the tool pages, and an
upload's page ("Add files", or a drop anywhere on it). A drop of `.torrent`
files only opens them, as before; anything else loads `lib/upload.js` (a lazy
chunk). "Upload a folder" is a button inside the zone; its picker sits next to
the form, so a click on it opens the folder picker only. A guest gets the
sign-in card instead, from a drop and from that button alike.

`lib/upload.js` asks a token for the drop's total size and PUTs every file in
16 MiB parts, two files at once, to
`<url>/<path>?token=…&api-key=…&offset=…&size=…` — browser →
torrent-http-proxy → seeder. Path segments are NFC, `~` (the proxy's chain
separator) becomes `～`, at most 255 bytes. A new upload's root is the one
folder dropped, the one file's name without its extension, or
`Upload <date> <time>`. What the seeder, or the proxy in front of it, answers:

| Answer | The uploader |
|---|---|
| 204 / 201 | the part is taken / the file is complete |
| 409 `<n>` | resumes the file at byte n (the same folder dropped again resumes) |
| 409 `exists`, `deleted` | takes the next free name, `name (2).ext` (after a `deleted`, up to 5 times; then the file fails) |
| 409 `frozen`, 409 `root`, 410 | stops: the upload can no longer be written |
| 403 | renews the token once; a second 403 stops |
| 413 `size` | asks once more, for the files not started yet, and starts the file over; a second time it stops: the space ran out |
| 413 `files` | stops: too many files |
| 507 | the node is full: waits a minute |
| 429 | waits 5 s |
| 0, 502, 503, 504 | offline: retries with backoff up to 30 s |
| anything else | that file fails; the others go on |

The progress shows in the page's upload log (`partials/upload/log.html`,
above the search field on the home and tool pages (above the drop zone on
the uploads tool page, which has no search field), under the header on
`/<id>`): the same log as a torrent's load (`lib/progressLog.js`), fed by the
uploader instead of a job's stream. One line, "Uploading your files", with
files stored of files dropped and the percent of the bytes sent, which fills
the bar; a pause (offline, node full) takes the line's numbers. Under it, the
note that the upload is temporary. A stop is the run's verdict, with the
subscription upsell under a quota stop. When the space runs out during the
run, or on a 413 `files`, no new file starts but the ones in flight finish:
the line goes on with them, and the verdict comes when they are done. The
files that failed are named in a warning at the end, ten at most. "Open" (the upload's page, a new tab) shows
once the first file is stored, "Sign in" after a 401.

Done, it opens the upload's page (`/<id>`, `?pwd=` when it went into a
subfolder) the way a load does, with the log's redirect (an async link into
"main"), after waiting at most a second for its two Umami events
(`docs/analytics.md`, "Uploads"). When an async navigation took the log away,
the outcome is a 10 s toast instead. One run per tab
(`window.__uploadBusy`); other tabs of the browser see the run through a
mark in `localStorage` (`upload-busy:<id>`). "Save to Vault" is held in all of
them while the run lasts: a pledge closes the upload for writing.

Deleting a file or a folder on the page (`lib/uploadFile.js`, only while the
upload is not saved): a token (`want` 1), then a DELETE straight to the
seeder.

## The upload's page

`GET /<id>` for the owner, once rest-api answered for it (`keepUpload`): the
library row is kept with the upload's size and file count and its gone mark
cleared (`models.AddUploadToLibrary`); when the files changed since the last
enrichment (`torrent_resource.updated_at`), the stale enrichment and the
pictures are dropped and the upload is enriched again (the page waits up to
3 s). "Add files" while it is not in Vault, "Delete upload" for the owner; no
Share, no folder archive.

Anyone else gets the 403 card (`error.upload_private_sign_in`,
`error.upload_private_no_record`); the owner of an upload that is gone,
`upload.gone` with a 404 (`docs/user_errors.md`, `docs/status_and_caching.md`).

Saving to Vault seals the upload on the seeder first; its pledge cannot be
removed, only the upload deleted (`docs/vault.md`, "An upload's pledge").

## Deleting: `POST /upload/delete`

Signed-in only. A form with `resource_id` or `resource_ids` (comma-separated)
— the page's "Delete upload" (it asks first) and the bulk bar of "My uploads".
Each ID (`deleteUpload`):

1. Not an upload ID → error. Kind other than 0 → only the viewer's library row.
2. rest-api shows it to the viewer (the owner): every Vault pledge of it is
   removed and its `vault.resource` too (the VP come back); the seeder deletes
   it (`uploads.Delete`: 204 — tombstoned, it can never grow back; 410 —
   already gone; anything else is an error and stops here); the reserve is
   marked deleted (tombstoned: it weighs nothing at once); its pictures are
   erased; `models.PurgeResourceByID` takes every row.
3. rest-api does not show it (404, 403): the viewer's own pledge is released;
   a viewer who was ever given a token for it loses the pictures and the rows,
   anyone else only their library row.

Every step can be repeated: after a failure the same button carries on. One
upload failing does not stop the others; the first failure is what the viewer
reads. On success the viewer goes back to the page the form was on with
`toast.uploadsDeleted` — but from the deleted upload's own page, which is a
404 now, to `/lib/uploads` in the page's language.

## What goes when an upload goes

| When | Who calls | What web-ui erases |
|---|---|---|
| the owner deletes it | `POST /upload/delete` | its Vault copy, its files, its pictures, every row (`PurgeResourceByID`); `upload_reserve` stays, marked gone |
| a DMCA ban | NATS `resource.banned` (`handlers/event/banned.go`) | its files, its pictures, every row; retried every 30 s until all succeed (`docs/adult_classification.md`) |
| the cleaner evicts it | NATS `resource.uncached` without `file_idx` (`docs/cache_index.md`) | `uploads.Forget`, `certain` true: enrichment rows, pictures, watch history, user subtitle bindings |
| a quota check or the owner's page finds it gone | `Service.Quota`, `handlers/resource/get.go` `forgetGone` | `uploads.Forget`, `certain` false: enrichment rows and pictures only |
| its files changed | the owner's page | the stale enrichment and the pictures |
| the account is deleted | `handlers/profile` (`Service.ForgetUser`), before the account row | all of the `certain` set, for every upload the account was ever given a token for, saved or not |

`uploads.Forget` keeps the library row, the name in `torrent_resource` and
`upload_reserve` — the owner still reads "this upload is gone" — and leaves an
upload whose Vault copy is funded or stored alone: the copy outlives the
directory. `certain` false keeps the watch history and the subtitle bindings,
which nothing brings back, because a gone inferred from rest-api is wrong
while the upload's home node is away. Pictures (`thumbnail.Service.Erase`) are
keyed by content: a blob goes once no other resource's row names it
(`docs/share_previews.md`).

## The library: "My uploads"

`/lib/uploads` lists the account's uploads; the Torrents section and WebDAV
`torrents/` no longer do (`models.LibraryTorrents`), Movies and Series count
them like torrents, and the JSON API's `GET /library`, WebDAV `all/` and the
data export list them with the rest (`models.LibraryAll`, `docs/api.md`,
`docs/webdav.md`). "My uploads" sorts by date added, by last update
(`torrent_resource.updated_at`, which moves when the size or the file count
changes) and by name.

"Remove from library" does not take an upload out: it is deleted as a whole,
or its files, Vault copy and quota would be left with no link to them. The site
says so (`library.uploadKept`), the JSON API's `DELETE /library/{id}` answers
409 `conflict`.

## Analytics

`upload-start`, `upload-end` and `upload-gone-shown`, and the SQL that counts
uploads and saves: `docs/analytics.md`, "Uploads".
