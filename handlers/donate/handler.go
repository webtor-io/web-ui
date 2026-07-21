package donate

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	np "github.com/webtor-io/web-ui/services/payments"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

const patreonURL = "https://www.patreon.com/bePatron?u=24145874"

type Handler struct {
	tb   template.Builder[*web.Context]
	np   *np.Client
	jobs *j.Jobs
}

// RegisterHandler always serves /donate as a page: with a nil gateway client
// it renders without tier cards (Patreon only). A redirect here would break
// async navigation — nav links load /donate into #main via fetch, which
// cannot follow a cross-origin redirect to patreon.com.
func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], npClient *np.Client, jobs *j.Jobs) {
	h := &Handler{
		np:   npClient,
		jobs: jobs,
		tb:   tm.MustRegisterViews("donate/*").WithLayout("main"),
	}
	r.GET("/donate", h.index)
	r.GET("/donate/patreon", h.redirectPatreon)
	// Old checkout URL, now merged into /donate.
	r.GET("/donate/crypto", func(c *gin.Context) {
		c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/donate"))
	})
	r.POST("/donate/crypto", h.cryptoCheckout)
	r.GET("/donate/crypto/success", h.cryptoSuccess)
}

func (h *Handler) redirectPatreon(c *gin.Context) {
	c.Redirect(http.StatusTemporaryRedirect, patreonURL)
}

// tierMeta carries the marketing copy of the known tiers as i18n keys
// (translation-keys-in-Go pattern); unknown tiers still render a bare
// purchasable card.
type tierMeta struct {
	titleKey   string
	taglineKey string
	benefits   int
	// trial: the tier has a free trial on Patreon — the card links to the
	// Patreon block.
	trial bool
}

var tierMetas = map[string]tierMeta{
	"bronze": {"donate.crypto.tier.bronze.title", "donate.crypto.tier.bronze.tagline", 4, false},
	"silver": {"donate.crypto.tier.silver.title", "donate.crypto.tier.silver.tagline", 5, true},
	"gold":   {"donate.crypto.tier.gold.title", "donate.crypto.tier.gold.tagline", 5, false},
}

type tierCard struct {
	TierID      int
	Name        string
	TitleKey    string
	TaglineKey  string
	BenefitKeys []string
	Recommended bool
	HasTrial    bool

	HasMonthly bool
	MonthlyUSD string

	HasAnnual         bool
	AnnualPerMonthUSD string
	AnnualTotalUSD    string
}

type donateData struct {
	Cards []tierCard
	// AnnualSavePct labels the pay-annually toggle; 0 hides the saving hint.
	AnnualSavePct int
	// FreeMonths restates the annual discount as months of 12 not paid for
	// (25% → 3).
	FreeMonths int
	PatreonURL string
}

func fmtUSD(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func buildCards(prices []np.Price) *donateData {
	byTier := map[int]*tierCard{}
	monthlyRaw := map[int]float64{}
	order := []int{}
	savePct := 0
	for _, p := range prices {
		card, ok := byTier[p.TierID]
		if !ok {
			card = &tierCard{TierID: p.TierID, Name: p.TierName}
			if m, known := tierMetas[p.TierName]; known {
				card.TitleKey = m.titleKey
				card.TaglineKey = m.taglineKey
				card.HasTrial = m.trial
				for i := 1; i <= m.benefits; i++ {
					card.BenefitKeys = append(card.BenefitKeys,
						"donate.crypto.tier."+p.TierName+".b"+strconv.Itoa(i))
				}
			}
			byTier[p.TierID] = card
			order = append(order, p.TierID)
		}
		switch p.PeriodDays {
		case 30:
			card.HasMonthly = true
			card.MonthlyUSD = fmtUSD(p.AmountUSD)
			monthlyRaw[p.TierID] = p.AmountUSD
		case 365:
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
	if len(cards) > 0 {
		cards[len(cards)/2].Recommended = true
	}
	return &donateData{
		Cards:         cards,
		AnnualSavePct: savePct,
		FreeMonths:    int(math.Round(12 * float64(savePct) / 100)),
		PatreonURL:    patreonURL,
	}
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
		if p.TierID == tierID {
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
	if h.np == nil {
		tpl.HTML(http.StatusOK, web.NewContext(c).WithData(buildCards(nil)))
		return
	}
	prices, err := h.np.Prices(c.Request.Context())
	if err != nil {
		// Plans unavailable must not take the Patreon option down with it.
		tpl.HTML(http.StatusOK,
			web.NewContext(c).WithData(buildCards(nil)).WithErr(errors.Wrap(err, "failed to get plans")))
		return
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(buildCards(prices)))
}

func (h *Handler) cryptoCheckout(c *gin.Context) {
	donatePath := i18n.LangPath(i18n.GetLang(c), "/donate")
	if h.np == nil {
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
		prices, _ := h.np.Prices(ctx)
		h.tb.Build("donate/index").HTML(http.StatusInternalServerError,
			web.NewContext(c).WithData(buildCards(prices)).WithErr(errors.Wrap(err, "failed to create invoice")))
		return
	}
	// Hosted checkout lives on the payment provider's domain.
	c.Redirect(http.StatusFound, inv.InvoiceURL)
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
