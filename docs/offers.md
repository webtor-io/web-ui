# Offers: what the app sells, and where the numbers come from

Every upsell in web-ui sells the same thing — the **promo plan** — and every number it
quotes is data. Nothing about a tier (speed, Vault Points, trial length, which plan we
push) lives in a template, a locale file or a Go constant.

## The catalog

`webhook GET /prices` is the storefront catalog. It has two lists:

- **`prices`** — the plans on sale, one per (tier, period): `amount_usd`, `available`,
  and the offer terms `trial_days` (> 0 = the plan starts with a free trial that long)
  and `is_promo` (the one plan in-app offers sell; a partial unique index in the webhook
  DB allows at most one).
- **`tiers`** — what each tier grants, including tiers with no price (free):
  `download_rate` (Mbit/s), `vault_points`, `site_noads`, `embed_noads`. These are the
  very columns claims-provider reads when it grants a user their claims, so an offer
  promises exactly what the plan delivers. `null` = unlimited.

Where a fact lives follows one line: **`tier` is what you get, `price` is how you buy
it.** A trial is a property of a plan, not of a tier — Patreon fronts Silver *monthly*
with a trial while the annual plan of the same tier has none.

`trial_days` is what the storefront may *promise*; the trial itself is configured at the
membership provider. Change both together.

## Services

- `services/payments` — the HTTP client: `Catalog(ctx)` decodes `/prices` (short shared
  cache). A missing `tiers` key (older webhook) decodes as nil = "unknown", not empty.
- `services/offer` — turns the catalog into offers:
  - `Promo()` — the promo plan as an `Offer` (`RateMbps`, `VaultPoints`/`HasVault`,
    `TrialDays`, `URL`), or nil.
  - `HasPlans()` — there is something to sell at all.
  - `TrialDays(tier)` — the trial on that tier's plan, for post-purchase copy.
  - `Helper` — `promoOffer`, `hasPlans`, `downloadPitch` for templates.
  - The catalog is refreshed in the background (`Start()`), and a failed refresh keeps
    the last good one. Nothing on the stream or download path ever waits on the webhook.
- `handlers/donate` — `Checkout(c)` builds the provider checkout for a plan (trial
  variant included); `TierBenefits(tier, facts)` is the card's benefit lines.

Two rules fall out of this:

- **`TrialDays` > 0 only when the checkout can start the trial.** A trial nobody can
  begin is not offered — with Patreon off, the same plan is still sold, without it.
- **No catalog, no offer.** A deployment without the webhook (self-hosted) renders no
  upsell at all rather than a button pointing at someone else's storefront. This is the
  capability gate: not "is this self-hosted", but "is there a plan to sell".

## Surfaces

| Surface | Shown to | Says | Umami |
|---|---|---|---|
| Promo banner (`partials/extend.html`, deployment-provided) | not paying | the plan's speed and trial | `promo` |
| Download ready (`action/download_file.html`) | free | this file's wait now vs on the plan | `donate-download`, `donate-download-shown` |
| Cap modal (`action/errors/slow_download.html`) | free capped / paid capped | trial or speed / compare plans | `donate-slow-download` |
| Grace popup (`action/stream_video.html`) | free, after the grace window | trial or speed | `donate-grace` |
| No peers, dead swarm (`action/errors/no_peers.html`) | free, when the plan has Vault | Vault keeps the torrent | `donate-no-peers` |
| Onboarding locked steps | free | trial length | `onboarding-pro-*` |
| `/donate` cards | everyone | speed, Vault, trial plaque, RECOMMENDED | `donate-trial-plaque`, `donate-patreon-join` |
| `/speedtest` plans | everyone | tiers and caps from the catalog | `donate-speedtest` |

Each CTA carries `data-umami-event-target` = `trial` | `checkout` | `donate`, so the
funnel is readable per step, and links to the plan's own checkout when there is one —
`/donate` is the fallback, not the default.

`downloadPitch` prices a download in time: best case at the viewer's cap next to best
case on the plan ("4.3 GB takes about 2 h 3 min — about 12 min at 50 Mbps"). It returns
nothing when the file is small enough that the wait is under two minutes, when either
rate is unknown or unlimited, or when the plan is not faster — a wait nobody feels is
not an argument.

## Copy rules

- Quote what the plan grants and nothing else. The "no ads" line was removed from the
  cards and the download nudge in 2026-09: free accounts have seen no ads since the
  2026-06 experiment, so it sold something everyone already had.
- Day counts use CLDR plural keys (`offer.trialCta`, `donate.patreon.trialBanner`,
  `promo.speedTrial`) — 1 and 7 decline differently in half our locales.
- A number and its unit are one token with U+00A0 between them (`docs/i18n.md`).
- The trial plaque on `/donate` states what happens after the trial and where it is
  cancelled (`donate.patreon.trialAfter`): "how do I cancel" is the most common support
  request we get.

## Adding a surface

1. `{{ with promoOffer }}` — no offer, no markup.
2. Link `.URL` (fallback `/donate`), label with `offer.trialCta` when `.TrialDays`,
   else `offer.getRate` / a surface-specific key.
3. Add `data-umami-event` + `tier` + `target`, and an impression event if the click rate
   is going to be read.
4. Cover it in a render test with a catalog, without one, and with a trial the checkout
   cannot start (`jobs/scripts/offer_render_test.go`).
