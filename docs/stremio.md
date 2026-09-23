# Stremio addon

Webtor exposes a personal Stremio addon so a user's library (and any external
Stremio addons they wire in) is streamable inside Stremio. Routes live in
`handlers/stremio/`, business logic in `services/stremio/`.

## Endpoints

All under `/stremio` (`handlers/stremio/handler.go`):

| Route | Purpose |
|-------|---------|
| `GET /manifest.json` | Addon manifest (`resources: stream, catalog, meta`; `types: movie, series`) — see [Manifest text](#manifest-text) |
| `GET /catalog/:type/*id` | The user's library as a Stremio catalog |
| `GET /meta/:type/*id` | Series/movie meta. For series, `videos[]` is built from the library torrent's episodes (`Library.makeVideos`). The manifest declares no `idPrefixes`, so Stremio asks us about other addons' IDs too (`tmdb:series:76747`, `mx:…`): an ID we do not hold answers `null`, never an error — `parseLibraryID` takes season/episode only from a numeric tail, the rest is the series ID |
| `GET\|HEAD /resolve/*data` | Playback redirect. The JWT in the path carries `{hash, idx, exp}` (72h TTL — Stremio persists stream URLs across sessions and probes them on next-day resume/binge; 12h made those probes 401); resolves to a backend URL via `LinkResolver` and `302`s to it. A free account on a stream only Webtor could serve gets the [paywall clip](#the-paywall-clip) instead |
| `GET /stream/:type/*id` | Streams for a movie/episode (the pipeline below) |

`GET /configure` (Stremio's standard config entry): anonymous → login with
`return-url=/stremio/configure`; authenticated → `302 /profile#stremio`. The
anchor rides in the Location header — the only way it survives a login
round-trip — so anonymous-facing links (the tool-page CTA) point here instead
of at `/profile#stremio`.

## Manifest text

The description is what Stremio shows on the addon card and what addon
catalogues list (`services/stremio/manifest.go`). It says what the addon is:
the user's Webtor library plus the Stremio addons they added to their profile,
played through Webtor, with the torrent downloaded on Webtor's servers so the
user's IP does not join the swarm. It promises no speed of its own: "watch
them instantly" was dropped in 2026-09, because a stream starts once the swarm
has delivered enough of the file.

It quotes no plan numbers, and in particular not the site's free-plan cap: that
cap does not apply to the addon. `LinkResolver.ResolveLink` first tries the
streaming backends the user connected (TorBox, Real-Debrid — any signed-in
user, for files cached there), then falls back to Webtor's servers, which need
a paid plan (`requiresPayment`). So the description ends with "Playing through
Webtor's servers needs a paid Webtor plan." — true whichever backend the user
has. "Paid" is spelled out because Free is a plan too in Webtor's own words
(/donate, llms.txt). The landing page (`tool.webtorStremioAddon.description`
and the third benefit) and llms.txt say the same; none of them names a
streaming backend.

`manifestVersion` goes up whenever the text changes (0.0.2 → 0.1.0 with this
one): Stremio keeps the manifest it installed, and the version is what makes
clients and catalogues refetch it. The `stremioAddonsConfig` signature is
issued for the addon by its catalogue and was left as it is.

## Install flow (profile block)

`templates/partials/profile/stremio.html` + `assets/src/js/app/profile/stremio.js`.
Fresh account: one primary action, **Install in Stremio** — the generate form
mints the token and, with JS, the module opens the `stremio://` deep link as
soon as the async island re-renders into the token state. The intent has to
survive the round-trip (the fragment swap is a new subtree), so it rides in
`sessionStorage` (`stremio-install-pending`), set on submit by
`e.submitter`, consumed once on the next render. "Just give me the link" is
the same form without the intent (other device, Stremio Web). Without JS the
form posts, the page reloads into the token state and Install is the first
button there. Umami: `stremio-install-addon` with `stage=fresh|token|auto`,
`stremio-generate-addon-url` for link-only, `stremio-download-app`.
`/instructions/stremio` leads with this path and keeps the manual one below.
Review the fresh state on your own account: `/profile?preview=stremio-fresh`.

Token management (both `POST`, auth-gated, rendered by `templates/partials/profile/stremio.html`):

| Route | Purpose |
|-------|---------|
| `POST /url/generate` | Issues the addon token. **Idempotent** — `models.MakeAccessToken` keeps the existing token on conflict, so pressing it twice never breaks an installed addon |
| `POST /url/regenerate` | Rotates the token (`models.RegenerateAccessToken`). **Destructive**: the previous URL stops resolving immediately and the addon must be reinstalled on every device. Gated behind an expanded collapse in the UI |

Generation stays an explicit user action rather than something issued on first
profile view — partly as a signal of intent, and partly because
`access_token` with scope `stremio:read` is the metric for "user connected
Stremio" (see `docs/onboarding_empty_states.md`); auto-issuing would turn it
into "user opened the profile" and destroy the measurement.

The personalised addon URL is a short alias (`/s/<code>`, `services/url_alias`)
that points at `/token/<token>/stremio/`. The alias **must be created with
`proxy=true`** (`handlers/profile/handler.go` `getStremioAddonURL`) so addon
resources are served in place — a `proxy=false` alias `301`-redirects every
resource request, which some clients handle poorly.

> This was `false` in the code until the 2026-08 security pass, and the cost
> was not only client compatibility: the `301` carried the raw access token in
> its `Location`, so the Stremio client stored an account credential and
> replayed it on every request. Because `CreateOrGetURLAlias` matches on the
> URL, aliases minted before the fix keep `proxy=false` until the user
> regenerates the token — flipping the existing rows is a separate data fix.

## Stream pipeline

`Builder.BuildStreamsService` composes layers (inner → outer):

```
Library + AddonComposite + TorznabComposite   // library, addons, indexers
  → CompositeStream             // parallel fan-out, order preserved
  → DedupStream                 // dedupe by infohash (first wins)
  → PreferredStream             // keep only enabled resolutions …
  → LangFilterStream            // … and the preferred audio language
  → EnrichStream                // attach the /resolve URL + ⚡ cache marker
```

**Torznab indexers are a source, not a feature of their own.** They enter the
pipeline as one more `StreamsService` and every layer below treats them like
addon streams — see [torznab.md](./torznab.md). Two consequences worth
knowing here: the Torznab composite is appended *after* the addon composite
so `DedupStream` prefers the addon copy of a shared infohash (it carries a
file index the indexer cannot know), and indexers are gated by the same
`discover_only` setting as addons.

Per-service timeouts are no longer fixed at 5s: a source may implement
`TimeoutedService` to ask for its own budget (Torznab uses 12s, since a
Jackett query fans out to every configured tracker). `CompositeStream`
reports the max of its children, so nesting one composite inside another
does not clamp the inner budget back to the default.

**Language detection falls back to the script.** `ExtractLanguages` matches
explicit tags first (`rus`, `рус`, `ukr`, …); when a title carries none, a
Cyrillic title counts as Russian, or Ukrainian if it has letters Russian does
not (`і ї є ґ`), and the Russian-scene voice-over markers `AVO`/`MVO`/`DVO`
count as Russian on their own. Found in production: a user with Russian as
their preferred language had a rutracker release dropped from their Stremio
list because the title was transliterated English tagged only "AVO". Keep
`assets/src/js/lib/discover/lang.js` in sync — Discover shows the same chips.

**Library streams are exempt from PreferredStream and LangFilterStream.** They
carry a `webtorio|<resourceID>` bingeGroup (`libraryBingeGroupPrefix`); both
filters skip anything with that prefix, because the user already opted into
those exact torrents by adding them to their Vault. Without the PreferredStream
exemption a 4k library title — or a series episode whose filename carries no
resolution token (→ `"other"`) — silently vanishes from results.

### File index is persisted, not re-derived at /stream time

Each library `StreamItem` needs the torrent **file index** (`FileIdx`) — it
goes into the `/resolve` JWT and the ⚡ availability check, and is the
`content_id` rest-api's `/resource/<hash>/export/<idx>` expects.

`FileIdx` is stored on the `movie` / `episode` rows (`file_idx` column,
migration 60) at enrich time. The value comes straight from rest-api's
`ListItem.Index` — the file's position in the torrent's natural file order
(`r.Files`), authoritative and independent of the sorted/paginated `/list`
order. `Library.getStreamItem` reads it via `resolveFileItem` and derives the
filename from the path basename — **no rest-api call on the `/stream` hot
path**.

This replaced the old behavior where `retrieveTorrentItem` paginated
`/resource/<hash>/list` on every request, counting files until the path
matched. For files deep in large season packs that meant 2–3 sequential
rest-api round trips, and under the `CompositeStream` 5s per-service timeout it
intermittently dropped the **entire** Library result — vault streams silently
missing in Stremio, addon streams left on top. See migration 60 for the
rationale.

`resolveFileItem` falls back to the legacy `retrieveTorrentItem` list-walk when
`file_idx` is `NULL` (rows enriched before the column existed). The fallback is
nil-safe (a path no longer in the listing yields no stream, not a nil-deref)
and returns the matched item's **`ListItem.Index`** — the torrent's natural
file index — not a positional count over the sorted list. The old positional
count resolved the *wrong* file whenever the torrent's natural `r.Files` order
didn't match the folders-first/name sort (e.g. a season pack with scrambled
file order).

**Self-heal.** Adding a resource to a library calls `jobs.Enrich`, but enrich
short-circuits for already-enriched resources (`TryInsertOrLockMediaInfo`
returns nil), so a pre-migration-60 resource would keep `file_idx = NULL` and
fall back to the slow/legacy path forever. `Enricher.backfillFileIndex` closes
that gap: on the skip branch it fills `file_idx`/`file_size` directly from
rest-api `ListItem.Index/Size` (`models.FillFileIndex`), gated by
`HasNullFileIdx` so it issues no rest-api call once populated. A one-time
backfill seeds existing rows; self-heal covers everything added afterward.

> Cross-service note: `ListItem.Index` requires rest-api ≥ the release that
> added it. Bump the `github.com/webtor-io/rest-api` pin in `go.mod` after that
> release; until then enrich stores `file_idx` from a stale index field and the
> Library fast path may resolve the wrong file. Deploy rest-api first.

### Stream presentation (marker + title)

Library streams carry a `⭐` prefix on their `name` badge (added in
`EnrichStream.enrichStream` for any `isLibraryStream`), so the user's own
entries are unmistakable next to addon results.

`Library.makeStreamTitle` builds a Torrentio-style multi-line **`Title`** from
data already on the row — no extra fetch. It MUST go in `Title`, not
`Description`: Stremio (and addons like Torrentio) render `Title` and ignore
`Description` — a library stream that only set `Description` shows up in the
JSON but is invisible in the Stremio app.

```
The Big Bang Theory · S05E14 [2012 BluRay 1080p]
💾 1.41 GB  ⚙️ Library
🇷🇺 / 🇬🇧
```

- line 1 — clean title (+ `S<season>E<episode>` for series) and a
  `[year quality resolution]` tag built from the ptn snapshot (`md`), each part
  optional;
- line 2 — `💾 <size>` from the persisted `file_size` column (`bigint`,
  migration 60, captured from `ListItem.Size` at enrich time) + the `⚙️ Library`
  source. No 👤 seeders line — vault content is cached, not P2P;
- line 3 — language flags via `ExtractLanguages(filename)` (`lang.go`),
  de-duplicated, omitted when nothing is recognised. **Known gap:** ptn rarely
  populates `md.language` (~0.05% of rows) and the filename often carries no
  language token, so most flags are missing. The planned fix is to extract
  languages into `md` at enrich time and read from there — see
  `project_stremio_library_languages_in_md` memo.

## Binge-watching (auto-play next episode) — the non-obvious contract

This is easy to break and hard to diagnose, so read this before touching the
stream or resolve handlers. Mechanics are in `stremio-core`
(`src/models/player.rs`, `src/types/resource/stream.rs`):

1. **Matching is `bingeGroup` string-equality only.** `Stream::is_binge_match`
   compares `behavior_hints.binge_group` of the playing stream to each candidate
   of the next episode; nothing else (not the source type, url, or infohash). So
   the bingeGroup must be **identical across episodes** — webtor keys it by the
   torrent (`webtorio|<resourceID>`), which is stable for a season-pack.
2. **Next-episode streams are pre-loaded eagerly** when the player opens: Stremio
   reuses the playing stream's request and swaps the video id to the next
   episode, then loads `/stream/...:S:E+1`. If that response isn't `Ready` with a
   matching bingeGroup, the next-episode button falls back to source-select.
3. **Stremio validates the chosen stream's playback URL with a `HEAD` request**
   before auto-playing. Our playback URL is `/stremio/resolve/<jwt>`, so
   **`/resolve` must answer `HEAD`** (it mirrors `GET` → `302`). Gin does not
   auto-register `HEAD` for a `GET` route; a `HEAD` 404 makes Stremio treat the
   next episode's stream as dead and bounce to source-select **every time**.
   Guarded by `TestResolveRouteAcceptsHEAD`.

P2P addons (e.g. Torrentio without debrid) play via Stremio's torrent engine and
skip the HTTP HEAD probe, so they binge even when an HTTP addon does not — a
useful tell when debugging: if Torrentio binges and webtor doesn't, suspect the
playback URL (HEAD reachability / non-404), not the bingeGroup.

## The paywall clip

A free account's click on a stream that only Webtor's own servers could play
(no enabled debrid backend of the user has the file cached, and Webtor's
backend needs a paid tier) used to end in a `404` from `/resolve`, which the
Stremio player shows as a bare playback error. Measured 2026-09-16..23: 354
such clicks a week, from 74 addon tokens — people who installed the addon
and pressed play. The click now plays a 12-second clip instead:

1. "This stream plays through Webtor's servers" / "To watch it, you need a
   paid Webtor plan";
2. "Start a free trial", **webtor.io/trial** in large type with a QR code (a
   TV cannot follow a link, a phone can), and the small print "On Patreon, use
   the same email or Patreon account you sign in to Webtor with, or the plan
   won't be linked to your account".

The small print is there because of how a tier reaches an account:
claims-provider looks the membership up by the Patreon ID when the Webtor
account signed in or was linked through Patreon, and otherwise by email. A
trial started under another email leaves the addon account free.

### When it plays

`LinkResolver.ResolveLink` names the paywall: it returns
`link_resolver.ErrPlanRequired` exactly when it falls through the user's own
backends to Webtor's and the tier does not include it. Before this it was a
`nil` result, the same as "nothing to play". `resolve` (step 6,
`handlers/stremio/paywall.go`) then redirects to the clip — `302` to
`<DOMAIN>/pub/stremio/paywall-<lang>.mp4?v=<digest>` — when all of these hold,
and answers the old `404` (and logs the old `no URL generated for resolve`)
otherwise:

- **The promo plan has a trial the checkout can start** (`offer.Promo()` non-nil
  and `TrialDays > 0`). No catalog (self-hosted) — nothing to sell, no clip.
  A plan without a startable trial — no clip either: the clip is static and
  says "start a free trial", so it follows the rule `offer.Offer` follows for
  every trial it quotes (docs/offers.md: a trial nobody can start is not
  offered). Production today: Silver monthly, 7-day trial.
- **A clip exists** for the account's language (`user_settings.lang`, read
  through `web.GetUserSettings`), or for English, which is the fallback for a
  missing or unrendered language. The clips are listed once at startup
  ("stremio paywall clips loaded" with the languages).

`HEAD` gets the same redirect as `GET`: it is Stremio's probe before
auto-playing the next episode (see the binge contract above), and a probe that
reached the clip plays the clip rather than bouncing to source selection.
Paid accounts, debrid hits, a result with no URL (`404`) and resolver errors
(`500`) are untouched.

`?v=` is the first 8 hex of the file's SHA-256, read at startup: a re-rendered
clip is a new URL for every cache on the way, CDN included. Without it a
changed file could be served from an edge copy of the old one until that
expired, and whether a `pub/` file is edge-cached at all depends on the
cookies of whoever fetched it first.

Every redirect logs `stremio paywall video` (Info) with `hash`, `file_idx`,
`lang` (of the clip), `method` and `user_hash` — `auth.LogHash`, the first 16
hex of `md5(user_id)`, which SQL can recompute as `left(md5(user_id::text),
16)` — and counts `webui_stremio_paywall_video_total{lang,method}`.

### `/trial`

`handlers/trial`. `GET /trial` (and `/<lang>/trial`, via the i18n prefix
routing) is the short link the clip prints:

| Catalog | Answer |
|---|---|
| promo plan with a direct checkout (`Offer.URL`) | `302` to it — Patreon's trial checkout when the plan has a trial. The visit's utm parameters are not passed on: Patreon drops them |
| promo plan, no direct checkout (`USE_PATREON=false`) | `302` to `/<lang>/donate?utm_source=stremio&utm_medium=video&utm_campaign=paywall`; utm parameters the visit brought win over these, anything else is dropped |
| no promo plan | `404` |

Registered before the resource catch-all (`/:resource_id`, which answers
`/trial` today with a redirect to the home page and `error.invalid_resource`).
Not in the sitemap, and `noindex` like every page the sitemap does not list.
The QR code encodes `https://webtor.io/<lang>/trial?` + `trial.PaywallUTM`
(no prefix for English).

Every visit logs `trial shortlink` (Info) with `target` (`checkout`, `donate`,
`none`), `lang`, the three utm values and, when the phone is signed in,
`user_hash`; and counts `webui_trial_shortlink_total{target,campaign}`
(`campaign`: `paywall`, `none`, `other`). This line is the measurement of the
click: nothing from the clip survives the trip through Patreon.

### Rendering the clips

`scripts/stremio_paywall_video/render.py` draws both screens from the
`stremio.paywall.*` keys of each `locales/<lang>.json` (Pillow, the site's
night palette, the logo polygons of `logo-night.svg`, the Comfortaa wordmark
embedded in `assets/src/styles/comfortaa.css`, Inter 4.1 downloaded once from
its pinned release and checked against a SHA-256 — the site's embedded Inter
is an ASCII subset) and encodes them with ffmpeg:

The short way, from the web-ui root — it sets up the venv, renders all eleven
clips and runs the test that checks them against the locales:

```sh
make paywall-clips
make paywall-clips ARGS="--lang ru --frames /tmp/paywall-frames"   # one language, review frames
make paywall-clips FFMPEG="docker run --rm -v $PWD:$PWD -w $PWD jrottenberg/ffmpeg:8-alpine"
```

A re-render with unchanged copy is byte-identical (checked 2026-09-23), so an
unexpected diff in `pub/stremio/` means the copy or the renderer changed. By hand:

```sh
python3 -m venv /tmp/paywall-venv
/tmp/paywall-venv/bin/pip install -r scripts/stremio_paywall_video/requirements.txt
# from the web-ui root; ffmpeg on PATH, or in Docker:
FFMPEG="docker run --rm -v $PWD:$PWD -w $PWD jrottenberg/ffmpeg:8-alpine" \
    /tmp/paywall-venv/bin/python scripts/stremio_paywall_video/render.py \
    [--lang ru] [--frames /tmp/paywall-frames]
```

The encode is the subset every Stremio player decodes in hardware — ExoPlayer
(Android, Android TV), libmpv (desktop), AVPlayer (Apple TV, iOS): H.264 High
@ level 3.1, 1280×720, yuv420p, BT.709, 25 fps, keyframe every 5 s; a silent
48 kHz stereo AAC-LC track (some TV players will not start a video without
audio); `moov` first (`+faststart`); 12 s (4.5 s screen 1, 0.5 s cross-fade,
7 s screen 2). About 290–340 KB per language, 3.5 MB for all 11; the script
refuses a file over 600 KB. With the same ffmpeg image the output is
byte-for-byte reproducible.

Each clip records a SHA-256 of the five texts and the QR link it was drawn
from (`comment` tag, `webtor-paywall-src:<hex>`).
`handlers/stremio/paywall_clips_test.go` recomputes it from the locale files
and `trial.PaywallUTM`, so **a copy edit or a utm change without a re-render
fails the build**; the same file checks that every locale has a clip and the
five keys, the container layout (ftyp, moov before mdat), codec, profile,
level, size, the audio track and the 10–14 s duration, and the copy rules
below. The served file (`video/mp4`, byte ranges, `noindex`) is checked in
`handlers/static/paywall_clip_test.go`.

Copy rules for `stremio.paywall.*`: **no numbers** — trial length, speed and
price are the catalog's, and a static clip cannot follow them; **no mention of
connecting one's own debrid backend** (owner's decision, 2026-09-23); no
"instantly". The test enforces the first two and the obvious spellings of the
third.

Known limit: the dark glow bands slightly in 8-bit video (visible as faint
rings on a bright screen). Dithering it away quadrupled the size; `aq-mode=3`
is what the encode does instead.

### Measuring it: saw the clip → opened /trial → got a plan within 7 days

Loki (`{namespace="webtor",app="web-ui"}`) has the first two steps, the
`web_ui` database the third.

1. **Saw the clip** — clicks and distinct accounts per week (`GET` only: the
   `HEAD` binge probe is not a view):

   ```logql
   sum(count_over_time({namespace="webtor",app="web-ui"} |= "stremio paywall video" | logfmt | method="GET" [7d]))
   count(sum by (user_hash) (count_over_time({namespace="webtor",app="web-ui"} |= "stremio paywall video" | logfmt | method="GET" [7d])))
   ```

   For step 3, export `user_hash` with its first `time` (`query_range` over
   `... | logfmt | method="GET" | line_format "{{.time}} {{.user_hash}}"`).
   Before this change the same clicks were the `Warn` "no URL generated for
   resolve"; that line now stands only for the cases that still 404.

2. **Opened /trial** — from the clip's QR code (`utm_campaign=paywall`) versus
   typed (`webtor.io/trial` carries no utm):

   ```logql
   sum by (target, utm_campaign) (count_over_time({namespace="webtor",app="web-ui"} |= "trial shortlink" | logfmt [7d]))
   ```

   Prometheus has both steps without the hashes:
   `sum(increase(webui_stremio_paywall_video_total{method="GET"}[7d]))`,
   `sum by (target, campaign) (increase(webui_trial_shortlink_total[7d]))`.
   A phone that scanned the code is usually not the account that clicked, so
   step 2 is a count, not a per-account join.

3. **Got a plan within 7 days of the first view** — `web_ui` DB. The step from
   free to a paid tier, a trial included (a trial grants the tier), writes the
   welcome notification `tier-welcome-<tier>` with the account's `user_id`
   and the time (`handlers/event/user.go`, on the webhook's `user.updated`
   event).
   `public."user".tier` is the tier now, without a date:

   ```sql
   SET statement_timeout = '60s';
   WITH v(user_hash, first_seen) AS (VALUES
       ('1677cad08bd5b077', timestamptz '2026-09-24 19:05:00+00')  -- from step 1
   )
   SELECT count(DISTINCT v.user_hash)                                   AS viewers,
          count(DISTINCT u.user_id) FILTER (WHERE n.user_id IS NOT NULL) AS got_a_plan_7d,
          count(DISTINCT u.user_id) FILTER (WHERE u.tier NOT IN ('', 'free')) AS paid_tier_now
     FROM v
     LEFT JOIN public."user" u ON left(md5(u.user_id::text), 16) = v.user_hash
     LEFT JOIN public.notification n
            ON n.user_id = u.user_id
           AND n.key LIKE 'tier-welcome-%'
           AND n.created_at >= v.first_seen
           AND n.created_at <  v.first_seen + interval '7 days';
   ```

   `got_a_plan_7d` is a lower bound: the welcome is written only when the
   event finds the account still free, and a page request that syncs the
   tier first (`services/claims`) leaves no row. `paid_tier_now` is the
   cross-check, without the 7-day window.

   Money (the first charge after the trial) is in the webhook database: the
   member's first event with `patron_status = 'active_patron'` and
   `is_free_trial = 'false'`, matched by email — read it at day 7 + 7.

Blind spot, by construction: a viewer who starts the trial under another
email and never links Patreon shows up as a Patreon trial with no account to
join, which is exactly the case the small print is there to prevent.

## Where the bolt comes from

The cache index and who feeds it: `docs/cache_index.md`.
