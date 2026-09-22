package donate

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/offer"
	np "github.com/webtor-io/web-ui/services/payments"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	patreonURL         = "https://www.patreon.com/join/pavel_tatarskiy"
	patreonGiftURL     = "https://www.patreon.com/pavel_tatarskiy/gift"
	patreonCheckoutFmt = "https://www.patreon.com/checkout/pavel_tatarskiy?rid=%s"
	// patreonManageURL is where a member sees, changes and cancels the
	// membership — the page support keeps sending people to.
	patreonManageURL = "https://www.patreon.com/settings/memberships"
	// patreonCancelGuideURL is Patreon's step-by-step "cancel a paid
	// membership" article.
	patreonCancelGuideURL = "https://support.patreon.com/hc/en-us/articles/360005502572-Canceling-a-paid-membership"

	patreonFlag = "donate-patreon"
	cryptoFlag  = "donate-crypto"
)

// RegisterFlags adds the per-method payment toggles. donate-crypto is
// distinct from USE_PAYMENTS (the payments client): the client also serves
// the tier prices behind the card grid and the payment history, so it stays
// on when the crypto provider stops accepting new checkouts — this flag
// hides only the checkout offer.
func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolTFlag{
			Name:   patreonFlag,
			Usage:  "offer Patreon on the donate page (set to false to hide)",
			EnvVar: "USE_PATREON",
		},
		cli.BoolTFlag{
			Name:   cryptoFlag,
			Usage:  "offer crypto checkout on the donate page (set to false to hide)",
			EnvVar: "USE_CRYPTO",
		},
	)
}

// Checkout builds direct Patreon checkout links for the offers
// (services/offer): the trial variant for a plan with trial days, the annual
// cadence for a yearly plan. nil when Patreon is off — offers then lead to
// /donate — and "" for a tier Patreon does not sell.
func Checkout(c *cli.Context) offer.Checkout {
	if !c.BoolT(patreonFlag) {
		return nil
	}
	return patreonCheckout
}

func patreonCheckout(tier string, periodDays int, trial bool) string {
	m, ok := tierMetas[tier]
	if !ok || m.patreonRid == "" {
		return ""
	}
	u := fmt.Sprintf(patreonCheckoutFmt, m.patreonRid)
	if periodDays == 365 {
		u += "&cadence=12"
	}
	if trial {
		// Patreon fronts the plan with the free trial itself.
		u += "&is_free_trial=true"
	}
	return u
}

// TierBenefits lists a tier's marketing lines — the same ones the donate
// card prints — so a welcome message can restate what was just bought
// without a second copy of the copy. Speed and Vault come from the tier's
// catalog facts (nil facts: those lines are left out rather than guessed);
// the rest is per-tier copy.
func TierBenefits(tier string, facts *np.Tier) []offer.Benefit {
	var out []offer.Benefit
	if facts != nil {
		switch {
		case facts.VaultPoints == nil:
			out = append(out, offer.Benefit{Key: "donate.tier.vaultUnlimited"})
		case *facts.VaultPoints > 0:
			out = append(out, vaultBenefit(*facts.VaultPoints))
		}
		if facts.DownloadRate == nil {
			out = append(out, offer.Benefit{Key: "donate.tier.speedUnlimited"})
		} else {
			out = append(out, offer.Benefit{Key: "donate.tier.speed", Rate: *facts.DownloadRate})
		}
	}
	if m, ok := tierMetas[tier]; ok {
		for _, k := range m.extraBenefits {
			out = append(out, offer.Benefit{Key: k})
		}
	}
	return out
}

// vaultBenefit restates Vault Points as storage (1 VP = 1 GB), in TB from a
// whole thousand up — "1000 Vault Points (1 TB)". The unit is in the key so
// each language writes its own ("ГБ").
func vaultBenefit(vp int64) offer.Benefit {
	if vp >= 1000 && vp%1000 == 0 {
		return offer.Benefit{Key: "donate.tier.vaultTB", VP: vp, TB: vp / 1000}
	}
	return offer.Benefit{Key: "donate.tier.vaultGB", VP: vp}
}

// Billing is what the rest of the app may say about payments: with Patreon
// on, subscriptions are managed there; with it off there is no provider to
// point anyone at and the zero value keeps welcome mail silent on the
// subject. The trial length is not here — it is a plan's term, read from the
// catalog when a message needs it.
func Billing(c *cli.Context) notification.Billing {
	if !c.BoolT(patreonFlag) {
		return notification.Billing{}
	}
	return notification.Billing{Provider: "Patreon", ManageURL: patreonManageURL, CancelGuideURL: patreonCancelGuideURL}
}

type Handler struct {
	tb        template.Builder[*web.Context]
	np        *np.Client
	jobs      *j.Jobs
	patreonOn bool
	cryptoOn  bool
}

// RegisterHandler always serves /donate as a page: with a nil gateway client
// it renders without tier cards (Patreon only). A redirect here would break
// async navigation — nav links load /donate into #main via fetch, which
// cannot follow a cross-origin redirect to patreon.com.
func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], npClient *np.Client, jobs *j.Jobs) {
	h := &Handler{
		np:        npClient,
		jobs:      jobs,
		tb:        tm.MustRegisterViews("donate/*").WithLayout("main"),
		patreonOn: c.BoolT(patreonFlag),
		cryptoOn:  c.BoolT(cryptoFlag),
	}
	r.GET("/donate", h.index)
	r.GET("/donate/patreon", methodRedirect(h.patreonOn, patreonURL))
	// Old checkout URL, now merged into /donate.
	r.GET("/donate/crypto", func(c *gin.Context) {
		c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/donate"))
	})
	r.POST("/donate/crypto", h.cryptoCheckout)
	r.GET("/donate/crypto/success", h.cryptoSuccess)
	r.GET("/profile/payments", h.payments)
}

// methodRedirect keeps external storefront links behind same-origin routes,
// for umami tracking and URL changes in one place; a disabled method bounces
// back to /donate instead of 404ing stale links.
func methodRedirect(enabled bool, url string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/donate"))
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, url)
	}
}

// tierMeta carries the marketing copy of the known tiers as i18n keys
// (translation-keys-in-Go pattern) and their Patreon tier ids; unknown tiers
// still render a bare purchasable card. What a tier grants (speed, Vault) and
// how its plans are sold (trial, promo) are catalog data, not listed here.
type tierMeta struct {
	titleKey   string
	taglineKey string
	// extraBenefits: the tier's lines beyond speed and Vault — perks the
	// catalog does not model.
	extraBenefits []string
	// patreonRid is the Patreon tier id for direct checkout links.
	patreonRid string
}

var tierMetas = map[string]tierMeta{
	"bronze": {"donate.crypto.tier.bronze.title", "donate.crypto.tier.bronze.tagline", []string{"donate.tier.supporterBadge"}, "3981231"},
	"silver": {"donate.crypto.tier.silver.title", "donate.crypto.tier.silver.tagline", []string{"donate.tier.prioritySupport", "donate.tier.supporterBadge"}, "3972747"},
	"gold":   {"donate.crypto.tier.gold.title", "donate.crypto.tier.gold.tagline", []string{"donate.tier.prioritySupport", "donate.tier.supporterBadge"}, "3981014"},
}

type tierCard struct {
	TierID      int
	Name        string
	TitleKey    string
	TaglineKey  string
	Benefits    []offer.Benefit
	Recommended bool
	// TrialDays > 0: the monthly plan starts with a free trial this long
	// (and Patreon can start it) — the card shows the trial plaque, linked
	// to TrialURL.
	TrialDays int
	TrialURL  string

	HasMonthly bool
	MonthlyUSD string
	// MonthlyUnavailable: the crypto plan exists but sits below the payment
	// provider's minimum payment — the crypto option in the split-button
	// menu is greyed out for the monthly period.
	MonthlyUnavailable bool

	HasAnnual         bool
	AnnualPerMonthUSD string
	AnnualTotalUSD    string

	// Patreon direct-checkout links (default action of the Join button);
	// empty for tiers unknown to tierMetas.
	PatreonMonthURL string
	PatreonYearURL  string
}

type donateData struct {
	Cards []tierCard
	// PatreonEnabled gates the Patreon card below the tier grid, the
	// tier-card Patreon buttons, the trial plaque and the gift block
	// (USE_PATREON).
	PatreonEnabled bool
	// CryptoEnabled gates the "or pay with crypto" checkout links on the
	// tier cards (USE_CRYPTO); the cards themselves stay — their prices
	// come from our own DB, not the crypto provider.
	CryptoEnabled bool
	// HasUnavailable turns on the footnote about plans hidden because the
	// payment provider's minimum payment exceeds their price.
	HasUnavailable bool
	// AnnualSavePct labels the pay-annually toggle; 0 hides the saving hint.
	AnnualSavePct int
	// FreeMonths restates the annual discount as months of 12 not paid for
	// (25% → 3).
	FreeMonths     int
	PatreonGiftURL string
	// TrialDays / TrialTier: the trial the Patreon block advertises — the
	// one on the promo plan, else on any plan; 0 hides the badge. TrialTier
	// is display-ready ("Silver").
	TrialDays int
	TrialTier string
}

func fmtUSD(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func buildCards(cat *np.Catalog, patreonOn, cryptoOn bool) *donateData {
	var prices []np.Price
	if cat != nil {
		prices = cat.Prices
	}
	byTier := map[int]*tierCard{}
	monthlyRaw := map[int]float64{}
	order := []int{}
	savePct := 0
	hasUnavailable := false
	promoTier := -1
	trialDays, trialTier, trialIsPromo := 0, "", false
	for _, p := range prices {
		card, ok := byTier[p.TierID]
		if !ok {
			card = &tierCard{TierID: p.TierID, Name: p.TierName}
			if m, known := tierMetas[p.TierName]; known {
				card.TitleKey = m.titleKey
				card.TaglineKey = m.taglineKey
				card.Benefits = TierBenefits(p.TierName, cat.Tier(p.TierID))
				if patreonOn {
					card.PatreonMonthURL = patreonCheckout(p.TierName, 30, false)
					card.PatreonYearURL = patreonCheckout(p.TierName, 365, false)
				}
			}
			byTier[p.TierID] = card
			order = append(order, p.TierID)
		}
		if p.IsPromo {
			promoTier = p.TierID
		}
		switch p.PeriodDays {
		case 30:
			card.HasMonthly = true
			card.MonthlyUSD = fmtUSD(p.AmountUSD)
			if p.IsAvailable() {
				monthlyRaw[p.TierID] = p.AmountUSD
			} else {
				card.MonthlyUnavailable = true
				hasUnavailable = true
			}
			if p.TrialDays > 0 && patreonOn {
				if u := patreonCheckout(p.TierName, 30, true); u != "" {
					// The card's monthly Join starts the trial too: on
					// Patreon a trial plan has no other checkout.
					card.TrialDays, card.TrialURL, card.PatreonMonthURL = p.TrialDays, u, u
					if trialDays == 0 || (p.IsPromo && !trialIsPromo) {
						trialDays, trialTier, trialIsPromo = p.TrialDays, p.TierName, p.IsPromo
					}
				}
			}
		case 365:
			if !p.IsAvailable() {
				// No UI treatment for unavailable annual plans yet — they
				// are simply not offered.
				hasUnavailable = true
				continue
			}
			card.HasAnnual = true
			card.AnnualPerMonthUSD = fmtUSD(p.AmountUSD / 12)
			card.AnnualTotalUSD = fmtUSD(p.AmountUSD)
		}
	}
	sort.Ints(order)
	cards := make([]tierCard, 0, len(order))
	for _, id := range order {
		cards = append(cards, *byTier[id])
	}
	// Saving hint: annual vs 12 monthly payments, uniform across tiers by
	// pricing policy — take the first tier that has both.
	for _, p := range prices {
		if p.PeriodDays != 365 {
			continue
		}
		if m, ok := monthlyRaw[p.TierID]; ok {
			if full := m * 12; full > p.AmountUSD {
				savePct = int(math.Round((1 - p.AmountUSD/full) * 100))
			}
			break
		}
	}
	// The recommended card is the promo plan's tier. A catalog without one
	// (a webhook that predates offer terms) keeps the old rule, the middle
	// card, so the page does not change shape during a rollout.
	recommended := false
	for i := range cards {
		if cards[i].TierID == promoTier {
			cards[i].Recommended, recommended = true, true
		}
	}
	if !recommended && len(cards) > 0 {
		cards[len(cards)/2].Recommended = true
	}
	return &donateData{
		Cards:          cards,
		PatreonEnabled: patreonOn,
		CryptoEnabled:  cryptoOn,
		// The footnote explains greyed-out crypto options; without the
		// crypto links there is nothing to explain.
		HasUnavailable: hasUnavailable && cryptoOn,
		AnnualSavePct:  savePct,
		FreeMonths:     int(math.Round(12 * float64(savePct) / 100)),
		PatreonGiftURL: patreonGiftURL,
		TrialDays:      trialDays,
		TrialTier:      displayTierName(trialTier),
	}
}

// displayTierName is a tier id as a name in copy: "silver" → "Silver" (the
// brand the Patreon tiers carry in every language).
func displayTierName(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// pickPeriod resolves the billing period to what the card actually displayed:
// the annual price when the tier has one and the toggle was on, otherwise the
// monthly price. False means the tier has no purchasable period at all. On a
// prices-fetch error it falls back to the requested period — the webhook
// validates the pair anyway.
func (h *Handler) pickPeriod(ctx context.Context, tierID int, annual bool) (int, bool) {
	requested := 30
	if annual {
		requested = 365
	}
	prices, err := h.np.Prices(ctx)
	if err != nil {
		return requested, true
	}
	has := map[int]bool{}
	for _, p := range prices {
		if p.TierID == tierID && p.IsAvailable() {
			has[p.PeriodDays] = true
		}
	}
	switch {
	case annual && has[365]:
		return 365, true
	case has[30]:
		return 30, true
	case has[365]:
		return 365, true
	default:
		return 0, false
	}
}

// index shows the merged membership page: crypto tier cards plus Patreon as
// the secondary option. Anonymous users see it too — auth is only asked for
// on checkout.
func (h *Handler) index(c *gin.Context) {
	tpl := h.tb.Build("donate/index")

	var cat *np.Catalog
	var pricesErr error
	if h.np != nil {
		cat, pricesErr = h.np.Catalog(c.Request.Context())
	}
	data := buildCards(cat, h.patreonOn, h.cryptoOn)

	// The tier grid is built entirely from the payment provider's prices, so
	// with no provider -- unconfigured, or down -- there is nothing to choose
	// between and the page becomes an apology with one button under it. Send
	// the visitor to that button instead.
	//
	// Via our own /donate/patreon, not the storefront URL: it keeps the URL
	// and its umami event in one place. That route bounces back here when
	// Patreon is off, which is exactly why this redirect is conditional --
	// unconditional, the two would volley.
	if len(data.Cards) == 0 && h.patreonOn {
		c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/donate/patreon"))
		return
	}

	ctx := web.NewContext(c).WithData(data)
	if pricesErr != nil {
		// Plans unavailable must not take the Patreon option down with it.
		ctx = ctx.WithErr(errors.Wrap(pricesErr, "failed to get plans"))
	}
	tpl.HTML(http.StatusOK, ctx)
}

func (h *Handler) cryptoCheckout(c *gin.Context) {
	donatePath := i18n.LangPath(i18n.GetLang(c), "/donate")
	if h.np == nil || !h.cryptoOn {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// Checkout is auth-only: the invoice must bind to an account
		// (order_id → user) before the user reaches the payment page.
		// 302, not 307: the login page must be fetched with GET, not
		// re-POSTed with the checkout form. After login the user returns
		// to /donate.
		c.Redirect(http.StatusFound, "/login?from=donate&return-url="+url.QueryEscape(donatePath))
		return
	}
	// The card's Join button submits tier_id; the pay-annually checkbox
	// picks the period.
	tierID, err := strconv.Atoi(c.PostForm("tier_id"))
	if err != nil {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	periodDays, ok := h.pickPeriod(ctx, tierID, c.PostForm("annual") != "")
	if !ok {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	inv, err := h.np.CreateInvoice(ctx, &np.CreateInvoiceRequest{
		UserID:     u.ID.String(),
		Email:      u.Email,
		TierID:     tierID,
		PeriodDays: periodDays,
	})
	if err != nil {
		cat, _ := h.np.Catalog(ctx)
		h.tb.Build("donate/index").HTML(http.StatusInternalServerError,
			web.NewContext(c).WithData(buildCards(cat, h.patreonOn, h.cryptoOn)).WithErr(errors.Wrap(err, "failed to create invoice")))
		return
	}
	// Hosted checkout lives on the payment provider's domain.
	c.Redirect(http.StatusFound, inv.InvoiceURL)
}

// paymentStatusKey collapses provider statuses into the handful of
// user-facing labels the history page shows.
func paymentStatusKey(status string) string {
	switch status {
	case "finished":
		return "paid"
	case "partially_paid":
		return "partial"
	case "failed", "expired":
		return "failed"
	case "refunded":
		return "refunded"
	default:
		return "pending"
	}
}

// providerLabels maps provider ids to display names.
var providerLabels = map[string]string{
	np.Provider: "NOWPayments",
}

type paymentRow struct {
	Date       string
	DateISO    string
	Method     string
	TierName   string
	PeriodDays int
	AmountUSD  string
	StatusKey  string
	Pending    bool
	InvoiceURL string
	PaymentID  string
}

type paymentsData struct {
	Rows []paymentRow
}

// payments renders the user's crypto payment history (Patreon history lives
// on patreon.com).
func (h *Handler) payments(c *gin.Context) {
	donatePath := i18n.LangPath(i18n.GetLang(c), "/donate")
	if h.np == nil {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	tpl := h.tb.Build("profile/payments")
	items, err := h.np.ListPayments(ctx, u.ID.String())
	if err != nil {
		tpl.HTML(http.StatusInternalServerError,
			web.NewContext(c).WithData(&paymentsData{}).WithErr(errors.Wrap(err, "failed to get payments")))
		return
	}
	d := &paymentsData{}
	for _, it := range items {
		statusKey := paymentStatusKey(it.Status)
		name := it.TierName
		if name == "" {
			name = strconv.Itoa(it.TierID)
		}
		method := providerLabels[it.Provider]
		if method == "" {
			method = it.Provider
		}
		if it.PayCurrency != "" {
			method += " (" + strings.ToUpper(it.PayCurrency) + ")"
		}
		d.Rows = append(d.Rows, paymentRow{
			// UTC with an explicit marker as the no-JS fallback; the client
			// re-renders it in the browser's timezone via the datetime attr.
			Date:       it.CreatedAt.UTC().Format("02.01.2006 15:04") + " UTC",
			DateISO:    it.CreatedAt.UTC().Format(time.RFC3339),
			Method:     method,
			TierName:   name,
			PeriodDays: it.PeriodDays,
			AmountUSD:  fmtUSD(it.AmountUSD),
			StatusKey:  statusKey,
			Pending:    statusKey == "pending",
			InvoiceURL: it.InvoiceURL,
			PaymentID:  it.PaymentID,
		})
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(d))
}

type successData struct {
	Payment *np.Payment
	Job     *job.Job
	Pending bool
	Done    bool
	Partial bool
	Failed  bool
}

func (h *Handler) cryptoSuccess(c *gin.Context) {
	donatePath := i18n.LangPath(i18n.GetLang(c), "/donate")
	if h.np == nil {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	id := c.Query("payment_id")
	if _, err := uuid.Parse(id); err != nil {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// BTC confirmations can outlive the session — after login the user
		// must land back on this exact payment's status page.
		back := url.QueryEscape(i18n.LangPath(i18n.GetLang(c), "/donate/crypto/success") + "?payment_id=" + id)
		c.Redirect(http.StatusFound, "/login?from=donate&return-url="+back)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	tpl := h.tb.Build("donate/crypto_success")
	p, err := h.np.GetPayment(ctx, id)
	if err != nil {
		tpl.HTML(http.StatusInternalServerError,
			web.NewContext(c).WithData(&successData{}).WithErr(errors.Wrap(err, "failed to get payment")))
		return
	}
	// The success page (and the watch job it starts) is owner-only: the
	// payment must belong to the signed-in account.
	if p.UserID != u.ID.String() {
		c.Redirect(http.StatusFound, donatePath)
		return
	}
	d := &successData{Payment: p}
	switch p.Status {
	case "finished":
		d.Done = true
	case "partially_paid":
		d.Partial = true
	case "failed", "expired", "refunded":
		d.Failed = true
	default:
		d.Pending = true
		// Watch the payment server-side and stream progress to the page;
		// on a terminal status the job redirects back here and the
		// branches above render the outcome.
		watch, err := h.jobs.PaymentStatus(web.NewContext(c), h.np, id)
		if err == nil {
			d.Job = watch
		} else {
			log.WithError(err).Error("failed to start payment watch job")
		}
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(d))
}
