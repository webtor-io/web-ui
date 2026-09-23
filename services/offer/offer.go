// Package offer turns the storefront catalog into the offers the app shows:
// which plan an upsell sells, how fast it downloads, whether it starts with a
// free trial and where the checkout is.
//
// Every number an offer quotes comes from the catalog (webhook GET /prices):
// the plan's terms (trial_days, is_promo) from its price row, what it grants
// (download rate, Vault Points) from its tier row — the same columns the
// claims are built from, so an offer promises exactly what the plan grants.
// Nothing about tiers is hardcoded here.
//
// No catalog, no offer. A deployment without the webhook (self-hosted) has
// nothing to sell, and surfaces render no upsell at all rather than a generic
// one pointing at someone else's storefront. In production a webhook outage
// is invisible: the last good catalog stays in memory.
package offer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/payments"
)

const (
	// refreshInterval: how often the in-memory catalog is re-read. Plans
	// change by hand, rarely; the payments client adds its own short cache.
	refreshInterval = 5 * time.Minute
	// retryInterval: until the first catalog arrives, try again soon — a
	// pod that starts during a webhook blip should not sell nothing for
	// five minutes.
	retryInterval = 15 * time.Second
	// DiscountLead is how long a code must stay honoured after it is handed
	// out: a letter read two days late must not lead to a dead code, so a
	// code this close to its end is no longer offered at all.
	DiscountLead = 72 * time.Hour
)

// Source serves the storefront catalog — the payments client in production.
type Source interface {
	Catalog(ctx context.Context) (*payments.Catalog, error)
}

// Checkout builds the direct checkout link of a plan, trial variant included;
// "" when the plan cannot be bought directly (the membership provider is off,
// or it does not know the tier) — the offer then leads to /donate instead.
type Checkout func(tier string, periodDays int, trial bool) string

// Offer is one plan as an upsell presents it.
type Offer struct {
	Tier       string
	PeriodDays int
	// RateMbps is the plan's download cap; 0 = unlimited.
	RateMbps int
	// VaultPoints is the plan's Vault allowance; HasVault is false only for a
	// tier that grants none (nil in the catalog means unlimited, so true).
	VaultPoints int64
	HasVault    bool
	// TrialDays > 0 only when the plan has a trial AND the checkout can
	// start it — a trial nobody can start is not offered.
	TrialDays int
	// URL is the direct checkout (the trial checkout when TrialDays > 0),
	// or "" — then the offer links to /donate.
	URL string
}

// Benefit is one line of what a tier grants, as an i18n key plus the numbers
// it quotes (template usage: tp .Key "Rate" .Rate "VP" .VP "TB" .TB); static
// lines ignore the numbers.
type Benefit struct {
	Key  string
	Rate int64
	VP   int64
	// TB restates VP as terabytes for the vaultTB line (1 VP = 1 GB).
	TB int64
}

type Service struct {
	src      Source
	checkout Checkout
	cur      atomic.Pointer[payments.Catalog]
	stop     chan struct{}
	once     sync.Once
}

// New returns a service over src; a nil src (no webhook configured) never
// loads a catalog, so every offer is nil.
func New(src Source, checkout Checkout) *Service {
	return &Service{src: src, checkout: checkout, stop: make(chan struct{})}
}

// Start loads the catalog in the background and keeps it fresh. Not a
// cs.Servable on purpose: a servable that returns ends the process, and a
// deployment without a catalog has nothing to serve.
func (s *Service) Start() {
	if s.src == nil {
		return
	}
	go s.loop()
}

func (s *Service) Close() {
	s.once.Do(func() { close(s.stop) })
}

func (s *Service) loop() {
	for {
		wait := refreshInterval
		if !s.Refresh(context.Background()) && s.cur.Load() == nil {
			wait = retryInterval
		}
		select {
		case <-s.stop:
			return
		case <-time.After(wait):
		}
	}
}

// Refresh re-reads the catalog once. A failure keeps the previous catalog:
// an offer quoting a plan changed a few minutes ago is fine, an offer that
// vanishes whenever the webhook blinks is not.
func (s *Service) Refresh(ctx context.Context) bool {
	if s.src == nil {
		return false
	}
	c, err := s.src.Catalog(ctx)
	if err != nil {
		log.WithError(err).Warn("failed to refresh offer catalog")
		return false
	}
	s.cur.Store(c)
	return true
}

// Catalog is the last catalog loaded, nil before the first one.
func (s *Service) Catalog() *payments.Catalog {
	if s == nil {
		return nil
	}
	return s.cur.Load()
}

// HasPlans answers "is there anything to sell": a catalog with plans in it.
func (s *Service) HasPlans() bool {
	c := s.Catalog()
	return c != nil && len(c.Prices) > 0
}

// FreeRateMbps is the free tier's speed cap, 0 when there is no cap to state:
// no catalog, no free tier in it, or a free tier without a limit. Copy that
// quotes the cap reads it here rather than typing the number into a locale.
func (s *Service) FreeRateMbps() int {
	t := s.Catalog().TierNamed("free")
	if t == nil || t.DownloadRate == nil || *t.DownloadRate <= 0 {
		return 0
	}
	return int(*t.DownloadRate)
}

// Promo is the plan in-app offers sell (is_promo), nil when there is none or
// the catalog does not say what its tier grants — an upsell without its
// numbers would have to invent them.
func (s *Service) Promo() *Offer {
	c := s.Catalog()
	if c == nil {
		return nil
	}
	for _, p := range c.Prices {
		if p.IsPromo {
			return s.offerFor(c, p)
		}
	}
	return nil
}

// FreeCapMbps is the free plan's download cap in Mbit/s, 0 when the catalog
// quotes none: no catalog, no free tier in it, or a free tier without a cap.
func (s *Service) FreeCapMbps() int64 {
	t := s.Catalog().TierNamed("free")
	if t == nil || t.DownloadRate == nil {
		return 0
	}
	return *t.DownloadRate
}

// TrialDays is the trial of the tier's plan that has one, 0 when none does —
// for copy about a membership that may have started with it.
func (s *Service) TrialDays(tier string) int {
	c := s.Catalog()
	if c == nil {
		return 0
	}
	for _, p := range c.Prices {
		if p.TierName == tier && p.TrialDays > 0 {
			return p.TrialDays
		}
	}
	return 0
}

// Discount is the live discount code with the most time left, nil when the
// catalog has none that is still honoured DiscountLead after now. No code, no
// discount offer: nothing about a discount is configured outside the catalog.
func (s *Service) Discount(now time.Time) *payments.Discount {
	c := s.Catalog()
	if c == nil {
		return nil
	}
	var best *payments.Discount
	for i := range c.Discounts {
		d := &c.Discounts[i]
		if d.Code == "" || d.PercentOff <= 0 || (d.PeriodDays != 30 && d.PeriodDays != 365) {
			continue
		}
		if d.ExpiresAt.Before(now.Add(DiscountLead)) {
			continue
		}
		if best == nil || d.ExpiresAt.After(best.ExpiresAt) {
			best = d
		}
	}
	return best
}

// DiscountCheckout is where a discount code is typed in: the provider's
// checkout of the tier's plan for the code's period, without the trial — the
// code overrides it, and a former trialist cannot start another one. "" when
// the checkout cannot be built (no membership provider, unknown tier); the
// caller then points at the storefront.
func (s *Service) DiscountCheckout(tier string, d *payments.Discount) string {
	if s == nil || s.checkout == nil || d == nil || tier == "" {
		return ""
	}
	return s.checkout(tier, d.PeriodDays, false)
}

func (s *Service) offerFor(c *payments.Catalog, p payments.Price) *Offer {
	t := c.Tier(p.TierID)
	if t == nil {
		return nil
	}
	o := &Offer{Tier: p.TierName, PeriodDays: p.PeriodDays, HasVault: true}
	if t.DownloadRate != nil {
		o.RateMbps = int(*t.DownloadRate)
	}
	if t.VaultPoints != nil {
		o.VaultPoints = *t.VaultPoints
		o.HasVault = *t.VaultPoints > 0
	}
	if s.checkout == nil {
		return o
	}
	if p.TrialDays > 0 {
		if u := s.checkout(p.TierName, p.PeriodDays, true); u != "" {
			o.TrialDays = p.TrialDays
			o.URL = u
			return o
		}
	}
	o.URL = s.checkout(p.TierName, p.PeriodDays, false)
	return o
}
