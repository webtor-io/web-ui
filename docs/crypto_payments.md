# Crypto payments (NOWPayments)

Second way to get a paid tier alongside Patreon. Model: **prepaid periods** —
crypto has no pull payments, so instead of a subscription the user buys N days
of a tier; paying again stacks on top of the remaining days.

## Architecture

web-ui is a thin front. All payment state lives in the **webhook** service
(github.com/webtor-io/webhook), which owns the NOWPayments API keys, the
`crypto.*` tables in its DB, and the public IPN endpoint. The tier itself is
granted through the same `public.claim` view claims-provider already reads
(third UNION branch over `crypto.member`), so nothing changes in
claims-provider or its consumers.

```
/donate               fork page: Patreon | crypto (only when gateway configured)
/donate/patreon       302 → patreon.com (legacy behavior)
/donate/crypto        Patreon-style membership page (login required): one card
                      per tier (title/tagline/benefits from i18n keys in
                      handlers/donate, tierMetas map) + a pay-annually toggle
                      (checked by default). The toggle swaps monthly vs
                      per-month annual prices with CSS only (form is a `group`,
                      price blocks use `group-has-[:checked]:*`) — no JS.
POST /donate/crypto   fields: tier_id (card's Join button) + annual (toggle
                      checkbox) → period 365/30 → create invoice via webhook
                      internal API → 302 to NOWPayments hosted checkout
/donate/crypto/success?payment_id=X
                      status page; while the payment is pending it starts a
                      shared server-side watch job (jobs/payment.go, queue
                      "payment") that polls the invoice API and streams
                      progress to the page via the job SSE log (progressLog);
                      on a terminal status the job redirects back here and the
                      handler renders the outcome. No-JS fallback: <noscript>
                      meta refresh.
```

Unknown tier names in `crypto.price` still render a purchasable bare card
(no tagline/benefits); tiers present only with a monthly price keep showing
it in annual mode. The middle tier card is marked "Recommended".

After the on-chain payment finishes, webhook grants `crypto.member` and
publishes `user.updated` to NATS; web-ui's existing event handler drops the
cached claims, so the tier shows up within ~1 minute (claims cache TTL).

## web-ui pieces

- `services/nowpayments/client.go` — HTTP client for the webhook service's
  provider-agnostic invoice API (`PUT/GET /invoice/{id}`, `GET /prices`, no
  auth — cluster-internal); passes `provider: "nowpayments"` and a
  client-generated uuid (PUT is idempotent server-side). `New()` returns nil
  when payments are disabled or the webhook service address is missing.
- `handlers/donate/handler.go` — all routes above. With a nil client the page
  still renders (Patreon-only, no tier cards); checkout routes redirect back.
- `templates/views/donate/{index,crypto_success}.html` — i18n keys `donate.*`
  in all 11 locales.

## Config

| Env | Meaning |
|---|---|
| `USE_PAYMENTS` | Feature switch (values: `payments.enable`). Off → /donate renders Patreon-only |
| `WEBHOOK_SERVICE_HOST` / `WEBHOOK_SERVICE_PORT` | Webhook service address, auto-injected by kubernetes (same namespace); set manually for local dev |

Payment statuses shown on the success page: `finished` → done;
`partially_paid` → contact support (no automatic grant); `failed`/`expired`/
`refunded` → failed; anything else → pending with auto-refresh.

## Notes

- The user must be logged in: the invoice is bound to `user_id`/`email` at
  creation (`order_id` = payment uuid), so no email matching is involved.
- Payment history lives in the webhook DB (`crypto.payment`), not in the
  web-ui DB — it is intentionally outside the web-ui GDPR export; export of
  payment records is handled at the webhook service level if ever required.
