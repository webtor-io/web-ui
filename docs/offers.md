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
  - `Helper` — `promoOffer`, `hasPlans`, `downloadPitch`, `speedUp` for templates.
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
| Download ready (`action/download_file.html`) | free | the limit, this file's wait, "up to 10× faster" | `donate-download`, `donate-download-shown` |
| Cap modal (`action/errors/slow_download.html`) | free capped / paid capped | "watch without the speed cap" / compare plans | `donate-slow-download` |
| Grace popup (`action/stream_video.html`) | free, after the grace window | "keep watching at full speed" | `donate-grace` |
| No peers, dead swarm (`action/errors/no_peers.html`) | free, when the plan has Vault | "save to Vault" — never Mbps | `donate-no-peers` |
| Onboarding locked steps | free | trial length | `onboarding-pro-*` |
| `/donate` cards | everyone | speed, Vault, trial plaque, RECOMMENDED | `donate-trial-plaque`, `donate-patreon-join` |
| `/speedtest` plans | everyone | tiers and caps from the catalog | `donate-speedtest` |

Each CTA carries `data-umami-event-target` = `trial` | `checkout` | `donate`, so the
funnel is readable per step, and links to the plan's own checkout when there is one —
`/donate` is the fallback, not the default.

`downloadPitch` prices a download in time: best case at the viewer's cap next to best
case on the plan ("4.3 GB takes about 2 h 3 min. With a subscription — about 12 min").
It returns nothing under ten minutes of waiting ("3 min instead of 20 s" sells nothing),
when either rate is unknown or unlimited, or when the plan is not faster. Under a minute
it counts seconds, never "1 min" — rounding up made a 10× plan read as 3×.

`speedUp` is how many times faster the plan is than the viewer's cap (rounded down,
0 below 2×) — the number on the download button.

## The button

**The button says what the viewer gets; the line under it says it is free to try.**
"Try free for 7 days" named the mechanism and nothing tied it to the limit printed next
to it. Every upsell CTA is now outcome + risk remover:

| Surface | Button | Under it |
|---|---|---|
| Download | `offer.downloadUpTo` "Download up to 10× faster" | `offer.trialNote` "7 days free · cancel anytime" |
| Cap modal (stream) | `offer.watchUncapped` "Watch without the speed cap" | same |
| Grace popup | `offer.keepFullSpeed` "Keep watching at full speed" | same |
| No peers | `offer.saveToVault` "Save to Vault" | same |

The note appears only when the plan has a trial the checkout can start. The two
objections it answers are the two we see in support: paying for nothing, and not being
able to leave.

**Promise only what the plan guarantees.** A plan lifts the cap; it cannot make a slow
swarm fast. So the download button says "up to 10×", and the unqualified "10× faster"
(`offer.downloadFaster`) is reserved for a file already whole on our side
(`FileDownload.Cached`), where the cap is the only brake. The download title names the
limit — "Download speed is capped at 5 Mbps" — so the reader sees the problem before
the button offers the fix.

## Copy rules

- Quote what the plan grants and nothing else. The "no ads" line was removed from the
  cards and the download nudge in 2026-09: free accounts have seen no ads since the
  2026-06 experiment, so it sold something everyone already had.
- Day counts use CLDR plural keys (`offer.trialNote`, `offer.trialCta`,
  `donate.patreon.trialBanner`, `promo.speedTrial`) — 1 and 7 decline differently in
  half our locales; so does the ratio in Russian (`offer.downloadUpTo`: «в 2 раза»,
  «в 10 раз»).
- A number and its unit are one token with U+00A0 between them (`docs/i18n.md`).
- The trial plaque on `/donate` states what happens after the trial and where it is
  cancelled (`donate.patreon.trialAfter`): "how do I cancel" is the most common support
  request we get.

## Adding a surface

1. `{{ with promoOffer }}` — no offer, no markup.
2. Link `.URL` (fallback `/donate`); the button names the outcome for this surface,
   `offer.trialNote` goes under it when `.TrialDays` (see "The button").
3. Add `data-umami-event` + `tier` + `target`, and an impression event if the click rate
   is going to be read.
4. Cover it in a render test with a catalog, without one, and with a trial the checkout
   cannot start (`jobs/scripts/offer_render_test.go`).
