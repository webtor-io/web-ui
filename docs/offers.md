# Offers: what the app sells, and where the numbers come from

Every upsell in web-ui sells the same thing — the **promo plan** — and every number it
quotes is data. Nothing about a tier (speed, Vault Points, trial length, which plan we
push) lives in a template, a locale file or a Go constant.

## The catalog

`webhook GET /prices` is the storefront catalog. It has three lists:

- **`prices`** — the plans on sale, one per (tier, period): `amount_usd`, `available`,
  and the offer terms `trial_days` (> 0 = the plan starts with a free trial that long)
  and `is_promo` (the one plan in-app offers sell; a partial unique index in the webhook
  DB allows at most one).
- **`tiers`** — what each tier grants, including tiers with no price (free):
  `download_rate` (Mbit/s), `vault_points`, `site_noads`, `embed_noads`. These are the
  very columns claims-provider reads when it grants a user their claims, so an offer
  promises exactly what the plan delivers. `null` = unlimited.

- **`discounts`** — the discount codes the membership provider honours right now:
  `code`, `percent_off`, `period_days` (the plan length whose first billing period
  is discounted, in `price.period_days` units: 30 = first month, 365 = first year
  of a new membership, any tier) and `expires_at`. The code is typed in at the
  provider's checkout — Patreon gives no link that carries it. Only live codes are
  listed; an expired row stays in the webhook table as history. A missing key
  (older webhook) means no code.

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
  - `FreeRateMbps()` — the free tier's speed cap, 0 when there is none to state (no
    catalog, no free tier, or an uncapped one). For copy that quotes the cap — the
    `/watch-torrents-ios` comparison (`docs/tool_pages.md`) — instead of a number typed
    into eleven locales.
  - `Helper` — `promoOffer`, `hasPlans`, `freeRateMbps`, `downloadPitch`, `speedUp` for
    templates.
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
| Stremio paywall clip (`handlers/stremio/paywall.go`, `pub/stremio/paywall-<lang>.mp4`) | free, on a stream only Webtor's servers could play; only when the plan has a trial the checkout can start | "start a free trial" at `webtor.io/trial` (→ the plan's checkout) — no numbers, the clip is a static video | none: `stremio paywall video` / `trial shortlink` log lines and `webui_*` counters (docs/stremio.md) |
| Onboarding locked steps | free | trial length | `onboarding-pro-*` |
| `/donate` cards | everyone | speed, Vault, trial plaque, RECOMMENDED | `donate-trial-plaque`, `donate-patreon-join` |
| `/speedtest` plans | everyone | tiers and caps from the catalog | `donate-speedtest` |
| `/watch-torrents-ios` comparison (`about/sections.html`, `Cap`) | everyone | the free cap and that a plan raises it; only with `hasPlans` and a capped free tier | — |

Each CTA carries `data-umami-event-target` = `trial` | `checkout` | `donate`, so the
funnel is readable per step, and links to the plan's own checkout when there is one —
`/donate` is the fallback, not the default.

`downloadPitch` prices a download in time: best case at the viewer's cap next to best
case on the plan ("4.3 GB takes about 2 h 3 min. With a subscription — about 12 min").
It is shown for every file whose size is known, small ones too — the difference in time
should be felt on every download ("123 MB takes about 3 min. With a subscription — about
20 s"). It returns nothing only when the size (a partial archive) or a rate is unknown,
or the plan is not faster. Under a minute it counts seconds, never "1 min" — rounding up
made a 10× plan read as 3×.

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

## Discount codes and the winback letter

A discount code is created at the provider (Patreon: 5–90% off the first month or
year of a NEW membership, typed in at checkout, overrides the free trial, lives up
to 180 days) and recorded in the webhook's `discount` table — the webhook README
has the `INSERT`. Nothing about a discount lives in web-ui: no row, no offer.

`offer.Service.Discount(now)` is the live code with the most time left, and only
one still honoured `DiscountLead` (72 h) later — a letter read two days late must
not lead to a dead code.

The one surface today is the **winback letter** (`notification.SendWinBack`,
`templates/notification/winback.html`, keys `email.winback.*`): why the plan is
gone, the promo code on a line of its own ("enter it at checkout", valid through
the last day), and a button to the full-price checkout of the promo plan for the
code's plan length (`offer.DiscountCheckout`: Silver monthly today, no trial — the
reader cannot start another; `/donate` when no checkout can be built).

It goes out when a membership **ends without a single payment**
(`handlers/event/user.go` `winbackReason`, on `user.updated`, lifetime support a
known 0):

- **Trial cancelled** — the trial's `members:delete`, which lands exactly when the
  trial runs out (677 of 681 cancelled trials in 2026-07-20..08-31; the ~1.4% of
  trials Patreon removes early get the same letter). Off unless
  `WINBACK_TRIAL_ENDED_FROM` (RFC 3339) is set and passed; production turns it on
  2026-10-08 00:00 UTC so these letters do not leak into the offer checks of
  2026-10-01 and 10-08. The declined-card letters below start as soon as a code is
  live — their end event comes once, so holding them back would lose them — and
  the 10-08 check counts payments made after a membership had ended separately.
- **Card declined until Patreon gave up** — `former_patron` with the last charge
  `Declined`. About a quarter of such ends carry no charge status and are missed
  rather than confused with a cancelled trial.

Only the end counts: the codes are for people without a paid membership, and a
declined card keeps the membership in Patreon's retry state for a median 31 days.
It also keeps the discount away from the ~21% of declined members who had never
paid (first decline 2026-07-20..08-31) and paid full price later on their own.

Why: of the trials started 2026-07-15..09-07, 46% were cancelled during the trial
and 60% of the rest ended on a declined card. Both groups used the site like the
people who paid (a site account for ~93%, activity during the trial for 57–64%),
and the provider gives nobody a second trial.

**Once per account, ever**, whichever the reason. The letter does not go through
`Send`, whose 24h feed guard would reuse a row another pod just wrote and mail it
too. The row is the claim: the insert either succeeds — that call mails — or
hits the unique index of migration 74 (`notification (user_id) WHERE key =
'winback'`) because another pod got there first. Any earlier entry ends it for
events. A row whose letter never left although it had an address (a failed send,
a pod stopped mid-send) is mailed by the daily `notification send` cron
(`SendOwedWinBacks`): rows 10 minutes to 48 hours old — the code in their body had
72 h left when written, so it still holds — mailed as written by the one caller
that wins `ClaimOwed` (an `updated_at` compare-and-set). The feed prune never
deletes the row.

**Control group.** `WINBACK_HOLDOUT` percent of eligible accounts get nothing
(default 0 — everyone: at worst the letter gives half a first month to someone
who would have come back anyway). The bucket is stable and SQL can recompute it:
`('x' || substr(md5(user_id::text), 1, 7))::bit(28)::int % 100 < <holdout>` is
held out, and held-out accounts are logged ("winback letter held out"). Without a
holdout the effect is read before/after: money (`campaign_lifetime_support_cents
> 0`) after the end of the membership, per reason.

Preview (dev only): `/notifications/preview/winback?lang=ru` (`reason=trial`,
`tier=`, `percent=`, `days=365`).

## Adding a surface

1. `{{ with promoOffer }}` — no offer, no markup.
2. Link `.URL` (fallback `/donate`); the button names the outcome for this surface,
   `offer.trialNote` goes under it when `.TrialDays` (see "The button").
3. Add `data-umami-event` + `tier` + `target`, and an impression event if the click rate
   is going to be read.
4. Cover it in a render test with a catalog, without one, and with a trial the checkout
   cannot start (`jobs/scripts/offer_render_test.go`).
