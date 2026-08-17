# PWA (installable web app)

Web-ui ships as an installable PWA in the "lite" configuration: polished
manifest and platform integrations, **no service worker**. Offline caching was
rejected deliberately — see "What is intentionally absent" below.

## What the manifest provides

Generated at `assets/dist/night/manifest.webmanifest`, served at
`/manifest.webmanifest` (linked from `templates/layouts/main.html`).

- **Identity**: name/short_name `Webtor`, EN description, `display: standalone`.
- **Colors**: `background_color`/`theme_color` = `#0a0e1a` (the `w-bg` token),
  matching `<meta name="theme-color">` in the layout.
- **`start_url: /?ref=pwa`** — installed-app launches show up in Umami with
  referrer `pwa`, so install adoption is measurable without extra code.
- **Icons**: regular (`android-chrome-*`) + maskable (`android-chrome-maskable-*`)
  + `apple-touch-icon.png` for iOS home screen.
- **`protocol_handlers`**: `magnet` → `/%s`. Desktop Chrome/Edge only — an
  installed PWA can register as the OS magnet-link handler. The encoded magnet
  URI lands as a single path segment and is handled by the existing
  `GET /:resource_id` magnet branch (`handlers/resource/handler.go`); no
  dedicated endpoint exists or is needed.
- **`share_target`**: `GET /share?title=&text=&url=`. Android only — the
  installed PWA appears in the system share sheet.

## Share flow (`GET /share`)

`handlers/resource/share.go`, two-level pattern:

- Level 1 `share()`: reads `title`/`text`/`url` query params, redirects.
- Level 2 `resolveSharePath()`: pure function; extracts a magnet URI (substring
  scan, cut at whitespace) or a bare 40-hex infohash (via
  `common.ResolveQueryHash`) from any of the three fields. Success → `302` to
  `/{canonical magnet}` (the existing magnet GET flow); nothing streamable →
  `302 /`.

Tests: `handlers/resource/share_test.go`.

## Platform support matrix

| Capability | Desktop Chrome/Edge | Android Chrome | iOS Safari |
|---|---|---|---|
| Install prompt | yes | yes (WebAPK) | manual "Add to Home Screen" only |
| `magnet:` handler | yes (`protocol_handlers`) | **no** (WebAPK gets https intent-filters only) | no |
| Share sheet target | n/a | yes (`share_target`) | **no** (Share Target API unsupported) |

iOS users are limited to pasting into the form. A possible future workaround is
a downloadable iOS Shortcut that accepts shared text and opens
`webtor.io/<magnet>` — content task, not a code change.

## How it is generated / wired

- `webpack.config.js` → `FaviconsWebpackPlugin`: `favicons` options set
  identity/colors/start_url; `logoMaskable` points at
  `assets/src/images/logo-night-maskable.svg` (logo with ~20% safe-zone padding
  on `#0a0e1a`); the `manifest` option merges `assets/src/manifest.base.json`
  (holds `protocol_handlers` + `share_target` — fields the favicons library
  cannot emit itself) into the generated manifest.
- `handlers/static/handler.go` serves manifest and all icon files from the site
  root (manifest icon paths are root-relative; robots.txt blocks `/assets/`).
- `services/web/robots.go` `isIndexableAsset` already allowlists the
  `android-chrome-*` / `apple-touch-icon*` prefixes.

Adding a new icon size or file means updating the root-serve list in
`handlers/static/handler.go`.

## What is intentionally absent

- **Service worker / offline caching** — web-ui is SSR-first and deploys are
  hard cutovers; the recovery guarantee "a page refresh always yields the
  current version" is load-bearing (see CLAUDE.md SSR hard-cutover rule). A
  fetch-intercepting SW would break it and every deploy would grow a tail of
  clients on stale bundles. Offline is also useless for streaming.
  A **push-only** SW (no fetch handler) is safe and is the expected shape if
  Web Push for release subscriptions is ever built.
- **`.torrent` share target** — requires `method: POST` + multipart and a CSRF
  exemption decision. Deferred until text sharing proves adoption (Umami
  referrer `pwa` + `/share` hits).
- **Manifest `screenshots`** (richer install dialog) — polish, add later if
  install adoption matters.
