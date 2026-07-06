# Staging mode

Two flags turn a web-ui deployment into a stage instance that cannot
leak into search results or be visited by accident:

| Flag | Env | Effect |
|------|-----|--------|
| `--staging` | `STAGING` | Forces `X-Robots-Tag: noindex` on **every** response: `IndexFollow` per-route opt-ins and the asset exemptions (favicons, `webtor.jpg`, `sitemap.xml`, `robots.txt`) are all suppressed. |
| `--redirect-domain` | `REDIRECT_DOMAIN` | Any request whose `Host` differs from the canonical host (taken from `DOMAIN`) gets a `302` to `REDIRECT_DOMAIN` + original request URI. Empty = disabled (production). |

Implementation: `services/web/robots.go` (noindex), `services/web/redirect.go`
(host redirect), wired in `serve.go` before all other Gin middleware except
the error logger.

## Why it works this way

- **Redirect in the app, not ingress-nginx.** The controller's annotation
  validation (v1.12+) rejects `$request_uri` in
  `temporal-redirect`/`permanent-redirect`, and a variable-free redirect
  would drop the path. Snippet annotations are disabled cluster-wide.
- **302, not 301.** Browsers cache 301 per-URL indefinitely; if hostname
  roles are ever reshuffled, cached 301s would keep bouncing clients.
- **noindex header, not robots.txt Disallow.** A crawler blocked by
  robots.txt never fetches the page and therefore never sees the noindex
  header — Google can still index a blocked URL from external links
  ("indexed, though blocked by robots.txt"). Crawling must stay allowed
  for the header to be honored, so staging serves the regular robots.txt.

## Production layout (web-ui-alt)

Configured in `infra/helmfile/values/web-ui-alt.yaml.gotmpl`:

- The stage instance answers on a deliberately unguessable hostname
  (`stage-*.webtor.cc`); `DOMAIN` points at it.
- The public `webtor.cc` resolves to the same ingress/service and is
  302-redirected to `https://webtor.io` by `REDIRECT_DOMAIN`.
- TLS for the stage host comes from an explicit wildcard Certificate
  (`*.webtor.cc`), NOT ingress-shim, for two reasons: a per-host cert
  would publish the stage hostname in Certificate Transparency logs, and
  putting `*.webtor.cc` into ingress `tls.hosts` would make dnslb sync a
  literal wildcard A-record to Cloudflare (dnslb reads TLS hosts too).
  `kubernetes.io/tls-acme` is therefore set to `'false'` on that ingress.
