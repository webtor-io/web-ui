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

`GET /<infohash>/status` (the badge's SSE) additionally requires
`token=<jwt>` since 2026-09-07: same signing, audience `torrent-status`,
subject = infohash, lifetime 1 hour, minted at page render into
`data-status-token` on `#torrent-status`. The CSRF check stays. Reason: one
harvested session cookie + CSRF pair was enough to open streams forever from
clients that never loaded the page (it is challenged at the edge), each stream
making a seeder load the torrent. Without a session secret the check is
skipped and the attribute is empty.

Renewal: when the stream is refused (token expired), `status.js` calls the
view's built-in `reload()` (`lib/async.js`, any element with
`data-async-layout`): the page URL is re-fetched with `X-Layout: {{ template
"resource/status_inner" $ }}`, the inner partial (badge + token) is swapped in
and the view re-inits — at most once a minute. A person's edge challenge clearance
lets that fetch through; a client that never loaded the page cannot renew.
