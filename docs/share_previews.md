# Share previews and the embed creative

What a link to webtor.io looks like when it is pasted into a chat or a social
network.

## Which image a page shares

| Page | `og:image` | `twitter:card` | Where |
|---|---|---|---|
| Homepage, the 20 tool pages | `/og-card.png` (brand card) | `summary_large_image` | `templates/views/index.html` block `og` → partial `og_card` |
| Every other page on the main layout (`/about`, `/donate`, legal, profile…) | `/og-card.png` | `summary_large_image` | `templates/layouts/main.html` block `og` (default) → partial `og_card` |
| Resource page (`/<hash>`) | `/lib/poster/<resource_id>/og.jpg` (1200×630, `services/poster_resolver`) | `summary_large_image` | `templates/views/resource/get.html` block `og` |

**The `twitter:card` tag belongs inside the `og` block, next to the image it
describes.** A view that brings its own image overrides the whole block, and
its card type with it. Until 2026-09 the layout printed `twitter:card=summary`
after the block, unconditionally, so a resource page said `summary_large_image`
and `summary` at once. `services/template/share_preview_render_test.go`
renders the head of the homepage, a tool page, the layout default and a
resource page and checks there is exactly one of each tag.

The `og_card` partial (`templates/partials/og_card.html`) writes `og:image`,
its type, width, height and alt (`meta.ogImageAlt`, translated into all 11
locales), and the `twitter:card`. Before 2026-09 the `og:image` everywhere was
the SVG favicon, which no platform renders as a preview.

## The brand card: `pub/og-card.png`

1200×630 PNG, about 120 KB (budget: 300 KB). Served at `/og-card.png` like
every file in `pub/` (`handlers/static`), and exempted from the default
`X-Robots-Tag: noindex` (`services/web/robots.go`, `isIndexableAsset`);
`handlers/static/pub_test.go` checks the served size, type and header.

Content: the logo (`assets/src/images/logo-night.svg`) and the `webtor`
wordmark, the line "Stream & download torrents in your browser", and under it
"magnet links · .torrent files · no client to install". Centred, so a square
crop of the middle still carries the message.

How it was drawn (Pillow 12 with raqm, fontTools with brotli; the whole image
is drawn at 2× and scaled down with Lanczos):

| Element | Font | Size | Colour | Position (1× px) |
|---|---|---|---|---|
| Background | — | — | `#0a0e1a` (`w-bg`) plus a radial glow centred at (600, 250), radius 560: `rgba(232,67,147,.18)` → `rgba(108,92,231,.09)` at 35% → transparent at 75% | full canvas |
| Logo | the two polygons of `logo-night.svg` (`#f670b3`, `#0f172a`) | 76×76 | — | top 92, left edge of the centred lockup |
| Wordmark `web` + `tor` | Comfortaa 300 (`assets/src/styles/comfortaa.css`) | 92 | `#f1f5f9`, `tor` `#e84393` | 26 px right of the logo, x-height centred on the logo |
| Line 1 "Stream & download torrents" | Inter, `wght` 800 (`assets/src/styles/inter.css`) | 70 | `#f1f5f9` | top 262, centred |
| Line 2 "in your browser" | same | 70 | 135° gradient `#e84393` → `#a29bfe` (`.gradient-text`) | top 348, centred |
| "magnet links · .torrent files · no client to install" | Inter, `wght` 500 | 28 | `#94a3b8` (`w-sub`) | top 480, centred |

The fonts are the site's own: the base64 WOFF2 in the two stylesheets, decoded
and saved as TTF with fontTools. The Inter subset covers ASCII plus `·`, `–`,
`—`, `©`; a tagline outside that set needs the full font.

**Replacing the card:** social platforms cache the preview image by URL, some
of them until the page is re-scraped by hand. A redrawn card should get a new
file name, with the partial and the test pointing at it; overwriting
`og-card.png` in place leaves old previews in circulation.

## The embed creative: `pub/webtor.jpg`

1280×720 JPEG: white Georgia Pro Black on black, "Watch this and / other
torrents at / webtor.io", the domain larger and underlined. Two users:

- the self-promotion slot of the embed player — the deployment-provided
  `templates/partials/extend.html` (`embed_ads`) points at `/pub/webtor.jpg`;
- the artwork of last resort for a resource page's share card: when a resource
  has neither a poster nor a thumbnail, `services/poster_resolver` renders this
  file into the 1200×630 canvas (`defaultOGBannerPath`).

The rendered canvas is cached on S3, and nothing expires it. Its key carries a
digest of the file (`poster/default-<sha256[:8]>/og.jpg`, `bannerCacheID`), so
a redrawn `webtor.jpg` reaches resource cards on the next miss; with the old
fixed key (`poster/default/og.jpg`, cached 2026-05-24) the previous artwork
would have been served indefinitely. The earlier creative said "instantly",
which a stream start that waits on the swarm cannot promise.

## CDN caching of these files

The origin answers a request without cookies with `Set-Cookie` (session,
language, the ingress affinity cookie), static files included, and the CDN
does not cache such a response. Files under `pub/` are therefore cached only
when the first request after expiry came from a browser that already had the
cookies: on 2026-09-23 `/pub/webtor.jpg` and the favicons were edge hits,
while `/webtor.jpg`, `/llms.txt` and `/pub/Sintel.jpg` bypassed the edge on
every request — and `/og-card.png`, fetched mostly by crawlers without
cookies, will too. Harmless at their volume (one static file per preview
fetch), but a changed image can still be served from an edge copy until it
expires: purge `/pub/webtor.jpg` and `/webtor.jpg` after replacing the
creative.
