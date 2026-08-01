# WebDAV

Read-only WebDAV view of a user's library, so they can mount it with
`rclone`, Finder, Windows Explorer, Cyberduck, etc. and browse/stream their
torrents as files. Paid-only (`claims.IsPaid`).

The filesystem itself is **not** WebDAV-specific — S3 serves the same tree, see
[s3.md](s3.md):

| Package | Role |
|---------|------|
| `services/vfs` | `FileInfo` / `FileSystem` / `HTTPError` — the protocol-neutral contract |
| `services/libfs` | the library tree: roots, library-backed content, torrent files |
| `services/webdav` | RFC 4918 — a vendored, trimmed fork of [go-webdav] under `internal/` |
| `handlers/webdav` | routing, token management, and the WebDAV-only `PrefixDirectory` |

`services/webdav` re-exports the `vfs` types as aliases (`type FileInfo =
vfs.FileInfo`), so code and docs here keep reading as WebDAV code.

[go-webdav]: https://github.com/emersion/go-webdav

## Token management

Both routes are `POST`, under `auth.HasAuth` + `claims.IsPaid`, and render into
`templates/partials/profile/webdav.html`:

| Route | Purpose |
|-------|---------|
| `/webdav/url/generate` | Issues the token. **Idempotent** — `models.MakeAccessToken` keeps the existing token on conflict, so a second press never breaks a mounted drive |
| `/webdav/url/regenerate` | Rotates it (`models.RegenerateAccessToken`). **Destructive**: the old URL stops authenticating at once and every device with the drive mounted must be reconnected |

Rotation is the circular-arrow button inside the same `join` row as the URL and
`Copy URL`; the form's submit is the rotation and is wrapped in
`onsubmit="return confirm(...)"`, copying is a `type="button"`. Same shape as
the Stremio addon block — see `docs/stremio.md`.

## Request routing

The URL handed to the user (see `handlers/profile/handler.go:getWebDAVURL`) is a
short alias, e.g. `https://webtor.io/s/<code>/webdav/`. A single client request
is rewritten twice, in-process, before it reaches the WebDAV handler:

1. **`/s/<code>/...`** — `services/url_alias` resolves `<code>` to the real
   target `/<at-param>/<token>/webdav/fs/` and re-dispatches via
   `gin.Engine.HandleContext` (proxy mode).
2. **`/<at-param>/<token>/...`** — `services/access_token` strips the token into
   a `?<at-param>=<token>` query param, rewrites `URL.Path` to the remainder
   (`/webdav/fs/webdav/...`) and re-dispatches again.
3. **`/webdav/fs/*rest`** — `handlers/webdav.Handler.handleWebDAV` runs. It does
   **not** use the rewritten `URL.Path`; it re-parses `c.Request.RequestURI`
   (the *original* client path, untouched by `HandleContext`) and serves that.
   So the path the filesystem sees is the client-facing `/s/<code>/webdav/...`.

### Why the path still works: the `webdav` separator

`PrefixDirectory` (`handlers/webdav/prefix.go`) splits the path on the literal
string `"webdav"` (the `Separator` it is built with). It is the one piece of the
tree that stayed WebDAV-only — S3 addresses `services/libfs` directly as bucket
+ key and needs no prefix. The user-facing URL
deliberately ends in `/webdav/`, so a request to `…/webdav/all/` splits into
prefix `…/webdav` + inner path `/all/`. The prefix is re-prepended to every
`href` in the response (`libfs.AddPrefix`) so clients get absolute,
round-trippable paths. Below `PrefixDirectory` (all in `services/libfs`):

- `RootDirectory` — the four virtual top-level dirs: `all`, `movies`, `series`,
  `torrents`. Listing `/` returns these; deeper paths route to a child by name.
- `ContentDirectory` — library-backed (`all`/`movies`/`series`); lists the
  user's torrents and delegates into `TorrentDirectory` for file contents.
- `TorrentLibraryDirectory` — the `torrents` view.
- `DebugDirectory` — wraps everything and logs every `Stat`/`ReadDir`/`Open`
  (`path=…`, `files=…`). This is how to see what a client actually requested in
  prod: `kubectl logs` and grep `msg="read dir"`.

## rclone / client compatibility (two hard-won invariants)

`rclone` is the primary client and the strictest. Two non-obvious things will
silently break listings if regressed — both are covered by tests in
`services/webdav/internal/server_test.go`.

### 1. PROPFIND/PROPPATCH body must parse without a `Content-Type`

rclone sends a valid XML PROPFIND body (Statfs/quota, directory listing) with
**no `Content-Type` header**. `DecodeXMLRequest` / `handlePropfind` must parse
the body regardless of the header — empty body ⇒ `allprop` (RFC 4918 §9.1),
non-empty ⇒ decode XML, malformed ⇒ 400. Gating on `application/xml` returns
`400 webdav: unsupported request body` and breaks `rclone mount` entirely.

### 2. The first `<propstat>` in each `<response>` must be the 2xx one

This is the subtle one. rclone, **when `vendor` is `owncloud`/`nextcloud`**
(a very common default), only inspects the status of the *first* `<propstat>`
of each `<response>` — `Prop.StatusOK()` reads `Status[0]`. If it isn't 2xx,
rclone **discards the entire entry** ("Ignoring item with bad status") and the
listing comes back empty with no error.

`NewPropFindResponse` therefore must emit the found props (200) **before** the
404 block for unknown props — see the two-pass loop in
`services/webdav/internal/server.go`. This mirrors `golang.org/x/net/webdav` and
keeps both lenient vendors (`other`, `fastmail`) and strict ones (`owncloud`,
`nextcloud`) working.

> Ordering is the actual fix, not "support every requested prop". A directory
> response always has a 404 block — `getcontentlength`/`getcontenttype` are
> file-only and `oc:*`/`nc:*` are vendor extensions we don't implement — so the
> 2xx-first ordering is required regardless of which props we expose.

### Properties we expose (`backend.propFindFile`)

- **All entries:** `resourcetype` (collection-or-not) and `displayname`
  (path basename).
- **Directories:** also `getlastmodified` — we have a real `ModTime` (virtual
  roots = now, library dirs = torrent `CreatedAt`). Without it clients show the
  epoch for every folder.
- **Files only:** `getcontentlength`, plus `getcontenttype` / `getetag` when
  set.

Everything else a client asks for (incl. `getcontentlength` on a directory and
the `oc:`/`nc:` extensions) falls into the trailing 404 propstat — which is fine
as long as it stays *after* the 2xx one (invariant above).

> Lenient vendors scan all propstats for a 200, so they tolerate either order —
> which is why this bug hid until someone used an owncloud/nextcloud remote.
> The symptom is specifically: `rclone lsd` shows entries with `vendor=other`
> but nothing with `vendor=owncloud`. To reproduce without prod, point rclone
> at a local `services/webdav.Handler` with a fake FS and
> `:webdav,url='…',vendor=owncloud:`.
