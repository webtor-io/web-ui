# Trackers in logs, and the private-upload count

A magnet's `tr=` values are tracker URLs, and a private tracker's passkey is
part of its URL: in the query (`?passkey=`, `?pk=`) or as a path segment. Its
`as=` / `xs=` can be the tracker's download link, the user's key in it. A
magnet reaches web-ui as a page address and as form input, so wherever web-ui
writes an address or a query down, all of that is kept out (2026-11).

## Where they are kept out

| Where | How |
|---|---|
| Access log, the job logger (message, location, rendered template body), recovery, "redirect with error", the error middleware's "request failed" / "user error shown", the session refresh interstitial | `helpers.RedactURL` keeps a magnet's `xt`, `dn` and `xl` and replaces every other parameter's value with `<redacted>`: `tr=`, `as=`, `xs=`, `ws=`, `x.pe=`, and the keys an unencoded tracker URL spills into the magnet (`&tr=http://t/a.php?uid=1&uk=…` makes `uk=` a parameter of the magnet). An allowlist, so a name nobody thought of is hidden too. A magnet is found as is (a magnet page's address; `magnet:` without `?`, as the `/magnet:` route builds its query; in HTML, where `&` is `&amp;`), and then only a quote or a tag ends it: a pasted line break or space does not. It is also found encoded once (`/ext/magnet?url=magnet%3A%3F…`, up to the next `&`) and twice (that address passed on as a parameter, up to `%26`). Its parameters are split at `&`, `%26` and `%2526` whatever the level, since a magnet can be encoded in part; a name with an encoded `&` loses what follows it. Outside a magnet, as before: `tr=` values, `passkey=` and `pk=`. Applying it twice changes nothing (an error is redacted, then the line carrying it). |
| An error for a refused query ("wrong resource provided") | `common.RefusedQuery`: `query_len=<n>`, and `query="…"` (through `RedactURL`, trimmed, cut to 200 bytes on a character boundary) only for a magnet or text without a `/`, so the refused-input analyses can still read titles and broken magnets. Anything else is or carries a link, and a private tracker's link carries the user's key in its path or under names `RedactURL` does not know (`torrent_pass=`, `authkey=`): any scheme (`udp://…/announce/<key>`, `wss://…`), none (`host.tld/<key>/announce`, `host.tld/announce.php?torrent_pass=…`), or inside text (`<title> https://…`). It keeps `query="scheme://host"` when it parses as a URL with a host (`www.` read as `http://`), and no `query` at all otherwise. A magnet is quoted only if no `/` is left after `RedactURL`: a `/` there is in its name (a link pasted after a magnet runs on into `dn=`), or the magnet is one `RedactURL` does not see (`magnet://?…`). The magnet parser's own error drops the URI `url.Parse` quotes. |
| The JSON API's "api request rejected" / "api request failed" (`libapi.WriteError`) | The error and the path go through `RedactURL`: `POST /resource` hands a magnet to rest-api as it came, and rest-api answers one its parser refuses with the parser's error, the URI in it; and a resource id in the path may be a magnet (rest-api's convention), which `URL.Path` holds decoded. |
| The dead-magnet card (`load/errors/magnet`) | The card is a `Custom` item of the load job: kept in the job store, replayed to whoever loads the same torrent in the same language through a log endpoint that needs no account (the job id is `sha1(infohash/lang)`). It carries `magnet:?xt=urn:btih:<hash>` only. The log host (`partials/load/progress.html`), rendered for this browser alone as the answer to its own submit, carries the query in `data-load-query`; `progressLog.js` puts it into the card's `input[data-load-query]` when the card arrives. An attribute, not a hidden input: the div-host redirect turns the host's hidden inputs into query params of the resource page. |
| Card bodies in the log | The job logger writes a `Custom` item's template name, never its body: a card is rendered from what the job was asked, and no pattern catches every escaped shape of that (attribute, script string). `RenderTemplate` bodies are still logged, through `RedactURL`. |
| Umami | `lib/umami.js` sends a magnet page — `/magnet:?…`, the same through the browser's `magnet:` handler (manifest `/%s`: the whole magnet encoded into the path), `/ext/magnet?url=…`, with or without a language prefix — as its bare path (`/magnet:`, `/ru/magnet:`, `/ext/magnet`), as the page and as the next page's referrer. Every other page keeps its query (UTM); `exclude_search` stays off. Nothing on the server sends to Umami. |

The trade-off of the card: when the host has no query, the retry submits the
infohash alone, and a magnet that only its trackers could resolve then has DHT
alone. That is Discover's modal (it submits the bare infohash and draws its own
host, so nothing is lost) and a page still running JavaScript from before the
change.

What is not covered here: rest-api's and magnet2torrent's own logs (other
repos); a refused query with a key but no `/` (`host.tld?torrent_pass=<key>`,
a link percent-encoded whole, `http%3A%2F%2F…`), which is quoted as text with
only `RedactURL` applied; and a magnet's name, which is kept whatever it
holds. The site's form sends rest-api `m.String()` of a magnet it parsed,
which `url.Parse` accepts, so rest-api's parser has no URI to quote there.

## The private-upload count

`private_torrent.LogUpload` writes one Info line for each `.torrent` with
`private=1` in its info dict that comes in, and nothing for anything else (a
public torrent, a magnet, junk). It is a measurement; the upload behaves as
before.

```
level=info msg="private torrent upload" event=private_torrent_upload user=<auth.LogHash or anon> infohash=<hex> trackers=<scheme://host[:port],…> source=<door>
```

- `trackers`: each tracker's scheme, host and port, lower-cased, once, in the
  torrent's order. Never the path, the query or userinfo; a URL that does not
  parse or has no host is left out.
- `source`, by door: `site` (the form, `handlers/resource` → `scripts.Load`),
  `embed` (the embed's `torrentUrl`, fetched by `jobs/scripts/embed.go` →
  `scripts.Load`), `api` (`POST /resource` of the JSON API), `webdav` and `s3`
  (a `.torrent` put into the library, `services/libfs`; the protocol passes its
  name to `libfs.New`).
- It is written when the file is received and parses, before it is stored: a
  submit that the job queue answers with a run already in flight for the same
  torrent counts too, and so does an upload the store then refuses. The embed
  fetches its `torrentUrl` once per run of its job, which is keyed by visitor
  session and hour, so `source=embed` counts views of an embed more than
  torrents.

```
{app="web-ui"} |= "private_torrent_upload"
```
