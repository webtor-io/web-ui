# Status codes and caching

What the site answers for a URL that does not name a page. Since 2026-09-23.

## Resource URLs that name nothing: 404 with the home page

`GET /:resource_id` (`handlers/resource/get.go`, `notFound`) answers **404
with the home page** — the form, and the reason above it — in two cases:

| Case | Key | Example |
|---|---|---|
| the ID is not one: no run of five hex digits (`common.SHA1R`) | `error.invalid_resource` | `/wp-login.php`, `/library`, `/index.html`, `/apple-touch-icon-precomposed.png` |
| rest-api has no torrent under it (answers 404) | `error.not_found` | a share link whose torrent is gone, a mistyped hash |

The body is `index.Render` with an empty `index.Data` — the same page the
visitor used to reach through the redirect, now at the URL they asked for;
the hidden `instruction` field stays empty, so a dead path never becomes a
tool instruction. The X-Robots-Tag is the route's default `noindex, follow`
(resource pages are not in the sitemap). JSON clients get
`404 {"status":"error","message":<key>}`; the async navigation renders the
404 body like any other (`async.js` does not look at the status). Each answer
logs `user error shown` with `err_key` and `surface=page`.

**Why.** Both cases used to be `302 /?err=…`, i.e. a 200 home page with
`index, follow`. A search engine cannot drop a URL that answers 200: in
September 2026 one index held 2,335 stale hash URLs, about 27% of them
answering that redirect. In the 24 h to 2026-09-23 12:00 UTC the two
branches took 2,223 (invalid, 688 of them one icon probe) and 1,079 (not
found) requests.

**What still redirects**, deliberately:

- a refused resource — banned or stoplisted, rest-api answers 403,
  `error.forbidden` (3,785 in the same 24 h). Its handling is a separate
  policy and did not change;
- a failure on our side (rest-api or the database did not answer): it says
  nothing about whether the URL is dead;
- every form error — `POST /` and the `/magnet:?…` GET that shares its
  handler — through `web.RedirectWithError`, unchanged.

A banned hash whose torrent has already left the store is indistinguishable
from an unknown one: rest-api answers 404 for both, so it gets the 404 page
with `error.not_found`, as it got that message before.

## `/?err=…` is noindex

The home page and the tool pages (`handlers/index`) set `X-Robots-Tag:
noindex` when the query carries `err` — the state of one visit after a
failed form, not a page. The canonical still points at the clean URL.

## Legal pages

`/legal/<name>` renders `templates/views/legal/<name>.html`; a name without a
view answers 404 with `error/page` and `error.page_not_found` (it used to be a
bare 500 from the template manager — `template.Manager.HasView` is the
check). `/legal/terms` is a 301 to `/legal/tos` in the request's language.
Aliases live in `handlers/legal/handler.go`.
