# Trackers in logs, and the private-upload count

A magnet's `tr=` values are tracker URLs, and a private tracker's passkey is
part of its URL: in the query (`?passkey=`, `?pk=`) or as a path segment. A
magnet reaches web-ui as a page address and as form input, so wherever web-ui
writes an address or a query down, the trackers are kept out (2026-11).

## Where they are kept out

| Where | How |
|---|---|
| Access log, the job logger (message, location, rendered template body), recovery, "redirect with error", the error middleware's "request failed" / "user error shown", the session refresh interstitial | `helpers.RedactURL` replaces every `tr=` value with `<redacted>`: read as is (a magnet page's own query, a magnet in HTML where `&` is `&amp;`) and inside an encoded magnet (`/ext/magnet?url=magnet%3A%3F…%26tr%3D…`, where the magnet's separator is `%26`). It also redacts `passkey=` and `pk=`: an unencoded tracker URL in a magnet spills its own query into the magnet's (`&tr=http://t/a.php?uid=1&passkey=…`). |
| An error for a refused query ("wrong resource provided") | Carries `query_len=<n>`, not the query. A refused query has no infohash to give. The magnet parser's own error drops the URI `url.Parse` quotes. |
| The JSON API's "api request rejected" / "api request failed" (`libapi.WriteError`) | The error goes through `RedactURL`: `POST /resource` hands a magnet to rest-api as it came, and rest-api answers one its parser refuses with the parser's error, the URI in it. |
| The dead-magnet card (`load/errors/magnet`) | The card is a `Custom` item of the load job: kept in the job store, replayed to whoever loads the same torrent in the same language through a log endpoint that needs no account (the job id is `sha1(infohash/lang)`). It carries `magnet:?xt=urn:btih:<hash>` only. The log host (`partials/load/progress.html`), rendered for this browser alone as the answer to its own submit, carries the query in `data-load-query`; `progressLog.js` puts it into the card's `input[data-load-query]` when the card arrives. An attribute, not a hidden input: the div-host redirect turns the host's hidden inputs into query params of the resource page. |
| Card bodies in the log | The job logger writes a `Custom` item's template name, never its body: a card is rendered from what the job was asked, and no pattern catches every escaped shape of that (attribute, script string). `RenderTemplate` bodies are still logged, through `RedactURL`. |
| Umami | `lib/umami.js` sends a magnet page — `/magnet:?…`, the same through the browser's `magnet:` handler (manifest `/%s`: the whole magnet encoded into the path), `/ext/magnet?url=…`, with or without a language prefix — as its bare path (`/magnet:`, `/ru/magnet:`, `/ext/magnet`), as the page and as the next page's referrer. Every other page keeps its query (UTM); `exclude_search` stays off. Nothing on the server sends to Umami. |

The trade-off of the card: when the host has no query, the retry submits the
infohash alone, and a magnet that only its trackers could resolve then has DHT
alone. That is Discover's modal (it submits the bare infohash and draws its own
host, so nothing is lost) and a page still running JavaScript from before the
change.

What is not covered here: rest-api's and magnet2torrent's own logs (other
repos). The site's form sends rest-api `m.String()` of a magnet it parsed,
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
  torrent counts too, and so does an upload the store then refuses.

```
{app="web-ui"} |= "private_torrent_upload"
```
