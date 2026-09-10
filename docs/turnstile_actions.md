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
- `assets/src/js/lib/turnstileAction.js`: capture-phase interception of the
  five forms, token into a hidden input, re-submit. Two widgets: a *warm*
  one rendered at page load into the hidden `#turnstile-action`
  (layouts/main), so the silent path costs one `execute()`; and, when
  Cloudflare wants a click, a *live* one rendered fresh inside the form's
  job-log block — a widget does not survive being moved in the DOM (checked
  on stage 2026-09-10: after `appendChild` the iframe is gone and the
  checkbox is dead).
- The moment a button is pressed the script writes a step into the form's
  job-log block (`#log-<id>`) in the job log's own markup — "Checking that
  you are not a robot" (`action.turnstileCheck`) with the pulsing dot — so
  the wait reads as the first step of the job and not as a dead button. The
  server's reply replaces the block: the job's log on success, the card on
  refusal. The live widget appears under that step.
- Signed-in accounts: the server does not check them, and neither does the
  client — `layouts/main` renders `#turnstile-action` only when `.User` has
  no auth, and the submit handler also steps aside when `window._userId` is
  set (the nav partial sets it; async navigation re-renders the nav). Known
  gap: sign-out is an async view, so a visitor who signs out and starts a job
  without a page load has no widget and gets the card once; a reload fixes it.
- Deadlines: the script missing → the form goes out at once without a token
  (fail closed: the server refuses, the card says to disable blockers or
  sign in); script loaded but silent → 15 s; checkbox shown → 120 s.
- The widget is a separate Turnstile widget in **managed** mode rendered with
  `appearance: interaction-only`. Invisible mode was tried first and rejected:
  it never shows the checkbox, so an unsure visitor just fails. The support
  form keeps its own managed widget.
- `handlers/action.logRefusal`: every refusal is one warning `turnstile
  refused job start` with `codes` (siteverify's `error-codes`;
  `missing-input-response` when no token came), `country` (`CF-IPCountry`),
  `ua`, `referer`, `action`. Loki: `{app="web-ui"} |= "turnstile refused"`.
  Read the codes before drawing conclusions from the 400 count on these
  endpoints: a client that never ran the widget and a person whose token was
  used twice both get a 400.

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
