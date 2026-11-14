# Umami context

Page views and events go to Umami through `assets/src/js/lib/umami.js`, set up
in `assets/src/js/app/layout.js`. What rides along with them comes from
`assets/src/js/lib/trackContext.js`.

## Session id

On pages rendered through `layout.js` every pageview, event and identify call
carries `id`: the gin session cookie id (`window._sessionID`, rendered in
`<head>`) hashed to a UUID v5 by `lib/umami.js`, the way the upstream tracker
keeps its `identity`. Umami keys the session by that `id` when present, else
by ip + user agent + month, so:

- Since this change (2026-10-16) events and `session_data` (`tier`,
  `ft_source`, …) share one session. Before it only `identify` sent the id and
  landed in a shadow session with zero events (7 of 43,598 event sessions in
  two days had any session data). Events before the deploy sit in ip + UA
  sessions and never join any `session_data`; the identify-only sessions have
  the same key as the post-deploy ones, so they live on and collect the events
  of returning cookies — a session's `created_at` may predate its first event.
- One browser is one visitor for the cookie's life (30 days, reset on every
  session save, `handlers/session/handler.go`; across IP and UA changes), not
  per ip + UA per calendar month. Expect ≈20% fewer visitors on ranges of a
  few days (30.9k cookie sessions against 38.8k ip + UA sessions in the same
  two days); the gap grows with the range length (ip + UA sessions re-key on
  every IP change and on the monthly salt, the cookie does not), and the
  main-site figure is somewhat smaller because embed viewers stay ip + UA
  sessions and sit in the 38.8k. Not measured on prod yet. Per-visit metrics
  (bounce, duration) go by `visit_id`; `session_id` is for funnels and
  `session_data` joins.
- The `session` row (country, region, city, browser, os, device, screen,
  language) is written at the first hit of the cookie and never updated
  (`createSession … ON CONFLICT DO NOTHING`). Umami's Countries/Browsers/OS/
  Devices breakdowns and any SQL joining `session` read first-seen values for
  the cookie's life — a VPN hop, a browser upgrade or a new monitor does not
  move the session. With ip + UA keying the row was at most a month old and
  the UA matched by construction. The retention CronJob
  (`infra/helmfile/charts/umami`) deletes `session` rows older than three
  months; a cookie that outlives its row is re-created on its next tokenless
  hit, so first-seen values refresh about every three months.
- `session_data` is upserted per key (Umami 3.1.0 `saveSessionData`), so the
  post-auth re-identify in `nav.js`, which carries no `ft_*` keys, leaves the
  first touch in place. Re-check on an Umami upgrade.
- The tracker also keeps the `x-umami-cache` token from each reply, as the
  upstream one does. The token is a closure variable and dies with the page,
  so it holds the 30-minute visit only within one page's lifetime (SPA
  navigations, events); a new full page load (SSR entries, the Patreon return)
  has no token and starts from the hour-bucket `visit_id`, exactly like the
  upstream tracker. Before this change the token was the whole JSON reply and
  was ignored, so every hit re-created the session and the visit was the hour
  bucket. What this does to the visit count is unmeasured until checked after
  the deploy.
- No `id`, ip + UA session as before, in three cases: analytics disabled; the
  cookie store (no `REDIS_*`, i.e. self-hosted without redis), where
  `sessions.ID()` is always empty — with the redis store the first request
  already has an id (`services/api/session_id_test.go`); and the embed
  (`app/embed/check.js`, `embed/index.js`) which inits the tracker without
  `id` **on purpose**: a third-party iframe gets a fresh gin session per load
  (Safari ITP), so an id there would be one visitor per load. Hence `/embed`
  pageviews and `play-embed` never join `session_data`, and a browser that
  both watches an embed and visits the site is two visitors.

## Magnet pages

A magnet page is sent as its bare path — `/magnet:` (also `/ru/magnet:`, and
the browser's `magnet:` handler, which encodes the whole magnet into the
path), `/ext/magnet` — as the page and as the next page's referrer: its
address is the magnet, and a private tracker's passkey is part of a tracker
URL. Every other page keeps its query. Before 2026-11 these rows carried the
whole magnet. See `docs/private_torrent_logs.md`.

The magnet card's own events carry no link either: `magnet-failed-shown`
sends kind and timing, `magnet-private-shown` and `magnet-private-upload` the
name alone (`TestMagnetErrorCardRenders` pins both), and the page they fire on
is cut as above.

## Private torrent refusals

rest-api's refusal of a private torrent is shown by one partial
(`partials/private/refusal`): on the resource page (`views/resource/private`,
a 403 from rest-api) and in the load job's card. Its events carry no user
data and no tracker:

- `private-refused-shown` `{code, reason}`: any refusal shown, with
  rest-api's code and reason as they came (`private_torrent` / `sign_in`,
  `private_tracker_refused` / `key`, …). One event for every case: there is
  no separate event per way out (sign in, upload). Declared on the card
  (`data-umami-impression`) and sent by `app/private_refusal.js` through
  `av`, which runs after `layout.js` has set `window.umami`: on a full page
  load (a shared link, a reload) as well as after an async swap. An inline
  script would run while the page is parsed, before umami exists, and lose
  it. Like every impression (`lib/impression.js`) it is sent once per page
  load per `{code, reason}`: a re-render is not a second refusal, another
  refusal is.
- `private-reupload-upload`: a `.torrent` file picked in the refusal's upload
  (a signed-in user without a record, or a tracker that refused or names
  nothing usable). A click that ends in "cancel" is not counted.

`TestPrivateRefusalPartialRenders` pins both; `lib/privateRefusal.test.js`
pins the full-load order.

The Save to Vault modal counts its own refusals of a private pledge, the same
way (`data-umami-impression`, `app/private_refusal.js`):

- `vault-private-refused-shown` `{code, reason}`: `{code: private_unavailable}`
  when the modal opens on a private torrent before private pledges are open on
  the server (`PRIVATE_ACCESS_ENFORCE` off: the text says so, there is no Store
  button); `{code: private_torrent, reason: no_record}` when a pledge came back
  refused because the user never added the torrent with their own tracker. The
  warning shown above the Store button once private pledges are open is not
  counted. `TestPledgeModalPrivate` pins all three.

## On every event and identify call

`eventDefaults()` is merged into the data of every `umami.track` and
`umami.identify`: `tier`, `is_authed`, `user_id`, `lang` and `is_referral`
(the visit came from a shared resource link, `utm_campaign=resource_share`;
kept in localStorage so the flag survives until the conversion).

## First touch (session data)

`firstTouch()` records where this browser first came to Webtor from and keeps
it in localStorage (`webtor.first_touch`), never overwritten. `layout.js`
passes it to `umami.identify`, so it is stored as **session data** — once per
session, not on every event:

| key | value |
|---|---|
| `ft_source` | `utm_source`; else the referring host without `www.` (`google.com`, `chatgpt.com`); `self` when the referrer is webtor.io itself; `direct` when there is none |
| `ft_medium` | `utm_medium`, or empty |
| `ft_path` | the landing path, an info hash written as `:hash` (`/ru/:hash`) |
| `ft_day` | the day it was recorded, `YYYY-MM-DD` |

Why: a visitor who finds Webtor in search, leaves and comes back directly to
pay shows up as "direct" in the paying session. The first touch keeps the
channel that brought them. `self` is the Cloudflare case: a challenge reloads
the page it stood in front of, so the page becomes its own referrer and the
real one is lost.

Limits:
- Browsers that visited before 2026-09-24 record their next visit, not their
  real first one, and nothing marks them: every `ft_day` is 2026-09-24 or
  later. Early on this understates how often the first touch differs from the
  paying visit; the share of such browsers falls as new visitors come in.
- Whether the first touch earns its keep: if it differs from the paying
  visit's source in fewer than 10% of paying sessions, it adds nothing and
  goes. The check of 2026-10-16 (`job-ft-check-1016`, "0 of 147 paying
  sessions have `ft_source`") is **invalid**: it ran on pre-deploy data, where
  `identify` landed in a zero-event session, so no paying session could carry
  any session data. Read it as "tracker bug", not "first touch useless".
  Re-check at least three weeks after the 2026-10-16 deploy with `start` = the
  deploy time — a new one-off job (`job-ft-check-11xx`, the same `ft-check.sql`,
  new `start`/`end`) has to be scheduled; none is yet.
- Per browser, not per person: another device or a cleared storage is a new
  first touch.
- Nothing is recorded when the analytics chunk does not load (blocked,
  `umami.disabled`).

## Querying

The home page `/` is stored with an **empty `url_path`** (about a fifth of all
page views). Match home as `coalesce(url_path, '') ~ '^(/[a-z]{2})?/?$'`, or
the English home lands among "other pages".

First touch of paying sessions, for example:

```sql
SELECT d.string_value AS ft_source, count(DISTINCT e.session_id)
FROM website_event e
JOIN session_data d ON d.session_id = e.session_id AND d.data_key = 'ft_source'
WHERE e.website_id = '<website>' AND e.event_name = 'subscription-started'
  -- from the 2026-10-16 deploy: earlier events sit in ip + UA sessions and
  -- never join session_data (see "Session id")
  AND e.created_at > '2026-10-16'
GROUP BY 1 ORDER BY 2 DESC;
```
