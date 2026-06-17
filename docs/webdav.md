# WebDAV

Read-only WebDAV view of a user's library, so they can mount it with
`rclone`, Finder, Windows Explorer, Cyberduck, etc. and browse/stream their
torrents as files. Paid-only (`claims.IsPaid`). Implementation lives in
`handlers/webdav/` (the virtual filesystem) and `services/webdav/` (a vendored,
trimmed fork of [go-webdav] under `internal/` plus our `FileSystem` interface).

[go-webdav]: https://github.com/emersion/go-webdav

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
string `"webdav"` (the `sep` passed to `NewFileSystem`). The user-facing URL
deliberately ends in `/webdav/`, so a request to `…/webdav/all/` splits into
prefix `…/webdav` + inner path `/all/`. The prefix is re-prepended to every
`href` in the response (`addPrefix`) so clients get absolute, round-trippable
paths. Below `PrefixDirectory`:

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
