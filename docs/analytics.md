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
  refusal is. `{code: upload, reason: sign_in}` is not rest-api's: it is the
  sign-in card `lib/drop.js` shows a guest who dropped their own files (or
  pressed "Upload a folder") on the home page or a tool page, where uploads
  need an account. The card waits in a `<template>` and is counted when it is
  shown, once per page load.
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

## Seeding after watching (fake door)

**Temporary: a fake door until 2026-11-30.** Owner's decision of 2026-11-15:
count demand for "keep seeding with my key after watching" (so a private
tracker does not count a hit-and-run) before building it. Nothing of it
exists. The door sits under the private torrent notice
(`resource/seed_door` in `views/resource/get.html`), so only someone who may
play the private torrent sees it, and only where Umami is configured
(`USE_UMAMI`): counting is all it is for. The button is the `<summary>` of a
`<details>` that opens on "Thanks! This feature is still in development" with
no request to the server; it cannot be closed again. Paid tiers read their
days (Bronze 3, Silver 7, Gold 14; any other paid tier, such as sparkling, is
shown as Gold), free gets "on paid plans" and a link to `/donate`.

- `seed-door-shown` `{tier}`: the door rendered. Declared on it
  (`data-umami-impression`) and sent by `app/private_refusal.js`, like the
  refusals above; once per page load per tier, so an async move to another
  private torrent in the same page load is not a second show.
- `seed-door-click` `{tier}`: the button pressed, i.e. the door opened. The
  summary hides once open, so there is no closing click to tell apart.
- `seed-door-tiers-click` `{tier}`: "See plans" in the free tier's thanks.

`tier` is the claims tier name, the same value `eventDefaults()` puts on every
event (sparkling stays `sparkling` even though the door shows it Gold). A
click can follow a show that was deduplicated, so compare distinct sessions,
not events. Filter `event_data` by the same `created_at` window, or the query
times out (no index on `website_event_id`):

```sql
WITH ev AS MATERIALIZED (
  SELECT event_id, event_name, session_id FROM website_event
  WHERE website_id = '<website>' AND created_at >= '<deploy>' AND created_at < now()
    AND event_name IN ('seed-door-shown', 'seed-door-click', 'seed-door-tiers-click'))
SELECT d.string_value AS tier, ev.event_name, count(DISTINCT ev.session_id)
FROM ev JOIN event_data d ON d.website_event_id = ev.event_id AND d.data_key = 'tier'
  AND d.created_at >= '<deploy>'
GROUP BY 1, 2 ORDER BY 1, 2;
```

To remove: the `resource/seed_door` template and its call in
`resource/private_notice`, the `private.seedDoor.*` keys in all locales,
`TestPrivateNoticeSeedDoor`, the `useUmami` stub in its test helpers if
nothing else needs it, the door's mention in the comment of
`assets/src/js/app/private_refusal.js` and in the comment of its case in
`assets/src/js/lib/umami.test.js` (the case itself is generic and can stay),
and this section.

## Uploads

The uploader (`lib/upload.js`) sends two events through `window.umami.track`;
no template declares them:

- `upload-start` `{files, sizeBucket, add}`: the first part of a run was
  accepted (PUT 204 or 201). `files` is the number of files dropped,
  `sizeBucket` their total size (`<100M`, `<1G`, `<10G`, `10G+`), `add` is 1
  for "Add files" on an upload's page (a resumed upload included), else 0.
- `upload-end` `{result, sizeBucket, minutes, add}`: once per run, on every
  way out. `result`: `done` (every file stored), `quota` (no token: the
  space is full), `size` (the space ran out mid-upload), `gone` (the upload
  can no longer be written: deleted, evicted, saved to Vault), `auth` (the
  session ended), `errors` (anything else, failed files included).

A run stopped before its first byte (`quota`) has an end and no start; a
closed tab has a start and no end: abandoned runs = starts − ends, by
session. A browser that blocks Umami sends neither.

The properties are numbers and buckets only: no id, no file or folder name.
The event's URL is the page's, as with a page view: on an upload's page it
carries the upload's id and `?pwd=/<name>/<folder>`.

`upload_reserve` (web-ui database) is the exact count of uploads started
and of their reserved size: the first token writes the row and deleting the
upload does not remove it. The events add what the server cannot see:
whether a run finished, how long it took, why it stopped.

There is no event for "saved to Vault". Uploads saved right now (a deleted
upload leaves this count: its pledge goes with it; a pledge that lost its
funding is not counted: the page says "Not saved" then):

```sql
SELECT count(*) FROM vault.pledge p JOIN upload_reserve r USING (resource_id, user_id)
WHERE p.funded;
```

The way to it, by the existing Vault events on an upload's page.
`vault-clicked` counts presses, not opened forms: it is also sent when the
press was held back because an upload was running in the tab (a toast is
shown instead of the form). The step that means "the form was open" is
`vault-store-confirmed`:

```sql
SELECT event_name, count(*) AS events, count(DISTINCT session_id) AS sessions
FROM website_event
WHERE website_id = '<website>' AND event_type = 2
  AND event_name IN ('vault-clicked', 'vault-store-confirmed', 'vault-pledge-confirmed')
  AND url_path LIKE '%/9c5db5752b7c0%'
  AND created_at > now() - interval '7 days'
GROUP BY 1;
```

The owner's "this upload is gone" page — the home page under a 404, with
`upload.gone` above the form — is counted:

- `upload-gone-shown`, no properties of its own (the common ones go with
  it, as with every event: see "On every event and identify call"):
  declared on the error block (`data-umami-impression` on `#index-error`,
  for that key only) and sent by `app/private_refusal.js`, like the
  refusals above; once per page load.
  Only the owner is shown that text; anyone else gets the plain 404. A
  hand-typed `/?status=error&err=upload.gone` shows it to anyone and is
  counted too. The event's URL is the page's: the id of the upload that is
  gone. `TestUploadGoneImpression` pins the markup.

The share of regret: sessions that found an upload gone against sessions
that finished one, over the same period. The event is the text shown to
the owner, not an eviction: it also counts an upload whose run broke off
before its first part reached the seeder (a closed tab, a lost
connection). The first token has already made its row in the owner's
list, there is no folder behind it, and its page says the same words. The
event has no properties of its own, so the two cannot be told apart, and
how many such runs there are has not been measured: read the share as an
upper bound of regret. It is a ratio of two flows, not a cohort. Filter
`event_data` by the same window, or the query times out:

```sql
WITH ev AS MATERIALIZED (
  SELECT event_id, event_name, session_id FROM website_event
  WHERE website_id = '<website>' AND event_type = 2
    AND created_at >= '<from>' AND created_at < '<to>'
    AND event_name IN ('upload-gone-shown', 'upload-end')),
done AS (
  SELECT DISTINCT ev.session_id FROM ev
  JOIN event_data d ON d.website_event_id = ev.event_id
    AND d.data_key = 'result' AND d.string_value = 'done'
    AND d.created_at >= '<from>' AND d.created_at < '<to>'
  WHERE ev.event_name = 'upload-end')
SELECT
  (SELECT count(DISTINCT session_id) FROM ev WHERE event_name = 'upload-gone-shown') AS gone_sessions,
  (SELECT count(*) FROM done) AS done_sessions;
```

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
