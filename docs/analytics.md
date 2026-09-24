# Umami context

Page views and events go to Umami through `assets/src/js/lib/umami.js`, set up
in `assets/src/js/app/layout.js`. What rides along with them comes from
`assets/src/js/lib/trackContext.js`.

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
  real first one. Compare channels only for `ft_day` from 2026-09-24 on.
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
  AND e.created_at > '2026-09-24'
GROUP BY 1 ORDER BY 2 DESC;
```
