# IndexNow

IndexNow lets a site tell search engines which URLs changed instead of
waiting for a recrawl. Bing, Yandex, Seznam, Naver and Yep share submissions
through `api.indexnow.org`; Google does not take part. Bing's index also feeds
the search inside ChatGPT and Copilot.

## The key

`INDEXNOW_KEY` (`--indexnow-key`) is the key of this host: 8–128 characters of
`a-z`, `A-Z`, `0-9`, `-`. With a key set, web-ui serves it as `/<key>.txt`
(`handlers/static/indexnow.go`); a search engine accepts a submission for the
host only when that file answers with the key. Without a key there is no file:
a deployment that did not register a key cannot be submitted for. A malformed
key fails the start.

The key is not a secret — anyone can read the file — so it lives in the
deployment values, not in a secret store.

## Submitting

Submit the URLs whose content changed, after the deploy that changed them —
not the whole sitemap on every deploy. Up to 10 000 URLs per request:

```bash
KEY=<key>
jq -n --arg key "$KEY" --rawfile urls urls.txt \
  '{host: "webtor.io", key: $key, keyLocation: "https://webtor.io/\($key).txt",
    urlList: ($urls | split("\n") | map(select(length > 0)))}' |
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://api.indexnow.org/indexnow \
  -H 'Content-Type: application/json; charset=utf-8' -d @-
```

`200` or `202` means accepted (`202`: the key is still being checked). `403`
is a key the file does not confirm, `422` a URL outside the host.

webtor.io answers scripted clients with a Cloudflare challenge, so read the
sitemap for `urls.txt` from inside the cluster
(`kubectl -n webtor port-forward svc/web-ui 18090:80`, `Host: webtor.io`).

URLs that now answer 404 (dead torrent pages) may be submitted too: the
engine recrawls them and drops them sooner.
