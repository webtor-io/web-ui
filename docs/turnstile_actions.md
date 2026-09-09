# Turnstile on job starts

Since 2026-09-09 an anonymous start of a job — `POST /download-file`,
`/download-dir`, `/preview-image`, `/stream-audio`, `/stream-video` — must
carry a Cloudflare Turnstile token in `cf-turnstile-response`. Signed-in users
are not asked. Without the action key pair (`TURNSTILE_ACTION_SITE_KEY` /
`TURNSTILE_ACTION_SECRET_KEY`) nothing is checked and no widget is rendered —
the capability gates the behaviour.

## Why

After the seeder stopped loading torrents for status requests (cold stats),
the only way left for a bot to make a seeder work is to start a job. A bot that
cannot run the invisible widget cannot start one. Real users see nothing;
Cloudflare may ask a click from a suspicious environment (VPN, proxy).

## Pieces

- `services/turnstile`: second key pair (`NewAction`), helper predicates
  `useActionTurnstile` / `actionTurnstileSiteKey`.
- `handlers/action.verifyAction`: verifier call for anonymous requests, the
  client address from `CF-Connecting-IP` (the ingress does not restore it).
  Failure answers the usual card, `error.turnstile_failed`, 400.
- `assets/src/js/lib/turnstileAction.js`: invisible widget in
  `#turnstile-action` (layouts/main), capture-phase interception of the five
  forms, token into a hidden input, re-submit. Fail closed after 6 s without
  the script: the server refuses, the card says to disable blockers or sign in.
- The widget is a separate Turnstile widget in **managed** mode rendered with
  `appearance: interaction-only`: nothing is shown while Cloudflare vouches
  silently; when it wants a click, the container is moved right under the
  submitted form and the checkbox appears there (a person then gets two
  minutes instead of six seconds). Invisible mode was tried first and
  rejected: it never shows the checkbox, so an unsure visitor just fails. The
  support form keeps its own managed widget.

## Testing the unhappy paths

Cloudflare's test keys work on any domain; put them into the stage values
(`values/web-ui-alt.yaml.gotmpl`, `turnstile.actionSiteKey` /
`actionSecretKey`) and `sync.sh --force web-stage`:

| Scenario | Site key | Secret |
|---|---|---|
| always passes silently | `1x00000000000000000000AA` | `1x0000000000000000000000000000000AA` |
| forces the interactive checkbox | `3x00000000000000000000FF` | `1x0000000000000000000000000000000AA` |
| token always refused (the card) | `1x00000000000000000000AA` | `2x0000000000000000000000000000000AA` |

Restore the real pair afterwards.

## Not covered on purpose

- `PUT /stream-video/<type>` (track switch on a running job) — not a start.
- Embed pages start jobs through `jobs/embed.go`, not these endpoints.
- API and Stremio paths — key-authenticated already.
