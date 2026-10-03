# .torrent download links

`GET /<infohash>.torrent` returns the torrent file. Since 2026-09-06 the link
rendered on the resource page carries a token: `/<infohash>.torrent?token=<jwt>`,
HS256 on the session secret, audience `torrent-file`, subject = infohash,
lifetime 6 hours (`handlers/resource/torrent_link.go`).

## Why

The bare URL was a free, permanent, anonymous host for `.torrent` files.
Torrent indexes (itorrents.org, limetorrents.*) listed webtor.io URLs as the
download link, and in September 2026 four torrents carrying single ~900 MB
`.exe`/`.scr` payloads were fetched 1.4 million times a week that way — the
likely reason behind Yandex's "unwanted software" flag on the domain, and
most of the Cloudflare Argo bill. A token that expires in hours makes the URL
useless as a hosted link while keeping the button on the page working.

## Behaviour

- Valid token: the file, `Cache-Control: private, no-store`.
- Missing, expired, wrong infohash or wrong audience: `302` to `/<infohash>`
  — the resource page, where a person finds a fresh button.
- Banned torrent: `404 not available` (was a 500).
- No session secret configured (`SESSION_SECRET` empty): links are plain and
  the check is skipped — the capability gates the behaviour, not a flag.

The authenticated API (`/api/v1/resource/{id}.torrent`, Bearer key) and
rest-api's `.torrent` endpoint are separate code paths and unchanged.

# Status stream token

`GET /<infohash>/status` (the transfer status's SSE) additionally requires
`token=<jwt>` since 2026-09-07: same signing, audience `torrent-status`,
subject = infohash, lifetime 1 hour, minted at page render into
`data-status-token` on `#torrent-status-block` (the inner block, next to its
`data-csrf`). The CSRF check stays. Reason: one
harvested session cookie + CSRF pair was enough to open streams forever from
clients that never loaded the page (it is challenged at the edge), each stream
making a seeder load the torrent. Without a session secret the check is
skipped and the attribute is empty.

Renewal (`lib/statusStream.js`, shared by `status.js` and `vault/progress.js`):
when the stream is refused (403: the token expired, or the CSRF pair no longer
matches the session cookie), the view's built-in `reload()` (`lib/async.js`,
any element with `data-async-layout`) re-fetches the page URL with `X-Layout:
{{ template "resource/status_inner" $ }}`; the inner partial (the status block
with a fresh token *and* a fresh CSRF token — both live on the block, so a
cookie changed since the page does not refuse the renewed stream again) is
swapped in and the view re-inits, drawing the last word it had until the new
stream speaks. At most once a minute: a refusal sooner waits for the rest of
the minute plus up to 5 s of jitter (until 2026-10-03 it was dropped, and the
status froze for good — 129 of 4218 page views in 6 h). Two renewals in a row
with no message after them and the view gives up: "Status unavailable"
(`resource.status.unknown`, rendered into `data-status-unknown`), the player's
lock withdrawn, the sticky bar down. A person's edge challenge clearance lets
the fetch through; a client that never loaded the page cannot renew.

A tab hidden for a minute closes its streams (the server keeps a stream's loop,
its Vault polling and its seeder and proxy subscriptions while it is open:
streams of an hour or more were 73% of all stream time on 2026-10-03);
visible again, they reopen — after a renewal if the token has expired by
then. The block keeps what it showed meanwhile.

Every page that opens the stream must render the token — the handler cannot
tell a page that forgot it from a bot. There are two: the resource page
(`#torrent-status`, `resource/status.js`) and `/vault`, whose live-progress
rows each carry `data-status-token` (`vault/progress.js`, template function
`statusToken <infohash>`). `/vault` was left out on 2026-09-07 and its rows
spun forever until 2026-09-24; `handlers/vault/render_test.go` now checks that
every progress row carries a token the stream accepts. Renewal there works the
same way: the pledges table sits in `#vault-pledges` with
`data-async-layout` = `{{ template "vault/pledges_table" $ }}`, and a refused
stream reloads the table with fresh tokens, by the same rules. Given up there,
the rows keep their last word: `/vault` is a signed-in page, and a CSRF pair
refused for good means the session is gone.

# Live and cold status

Since 2026-09-09 the seeder answers stats without loading a torrent nobody is
streaming (`live: false` in the event; see torrent-web-seeder README, "Stats
look but do not touch"). web-ui reads `live`; a missing field (older seeder)
counts as live. For a cold reply `judgeSwarm` says "paused" straight away and
never "checking" or "no seeders" — there is no swarm to judge. The status opens
its stream immediately again; the visibility/interaction deferral of
2026-09-07 was removed with it.
