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
