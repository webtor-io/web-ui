package scripts

import (
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/web"
)

// CapCard is what the player's buffering label draws by itself once the
// viewer has answered the grace popup: the lock "Buffering | [lock] 5 Mbps"
// at every wait from then on, and the card behind it (owner, 2026-09-27:
// "after a seek the 20-minute popup appeared; after I answered that I want
// slow, a plain Buffering hung with no lock"). The answer says the rest of
// the film is at the plan's cap, but the transfer status's own word on it --
// thp's verdict, and the stream box 8 s after it -- comes 8-15 s later, and
// the popup kept the lock down while it was up.
//
// The status's label stays the one source when it is there
// (lib/player/buffering-label.js capLock prefers it); this is its stand-in
// until it is, rendered with the same words: the same keys and the same
// number (statusview.RateLabel, CapLine), the promo plan through the same
// /trial surface (offer.FromPlayerLabel) in stream_video.html, which renders
// it on the <video> as data-cap-card-* (only with something faster on sale:
// Sells). The line with what the file needs is the element's
// data-status-stall-sub, as for the status's label.
type CapCard struct {
	// Rate is the viewer's cap as the lock says it (statusview.RateLabel,
	// the status's PlayerLabel.Rate).
	Rate string
	// CapMbps is the cap Sells compares the promo plan with.
	CapMbps float64
	// Line is the cap alone, "Without a subscription — up to 5 Mbps"
	// (statusview.CapLine): the card's line when the job does not know what
	// the file needs (no StatusStallSub) -- the status's box line without
	// the bitrate.
	Line string
	// Auth and Tier are the status view's props for the card's events
	// (statusview.View.Auth, View.Tier): "user" or "anon", the claims' tier
	// name or "free".
	Auth string
	Tier string
}

// Sells: the promo plan o is on sale and faster than the viewer's cap -- the
// status box's own test for a button (statusview.PromoFaster). Template
// usage: {{ if $card.Sells . }} inside {{ with promoOffer }}.
func (cc *CapCard) Sells(o *offer.Offer) bool {
	return cc != nil && statusview.PromoFaster(o, cc.CapMbps)
}

// NewCapCard is the card for the viewer of c, in their language; nil without
// a cap (no claims, no rate claim): there is no number for the lock to say.
func NewCapCard(svc *i18n.Service, c *web.Context) *CapCard {
	if svc == nil || c == nil || c.ApiClaims == nil {
		return nil
	}
	loc := svc.Localizer(c.Lang)
	capMbps := statusview.RateMbps(c.ApiClaims.Rate)
	rate := statusview.RateLabel(loc, c.Lang, capMbps)
	if rate == "" {
		return nil
	}
	cc := &CapCard{
		Rate:    rate,
		CapMbps: capMbps,
		Line:    statusview.CapLine(loc, c.Lang, !isFreeTier(c), capMbps),
		Auth:    "anon",
		Tier:    "free",
	}
	if c.User != nil && c.User.HasAuth() {
		cc.Auth = "user"
	}
	if n := c.Claims.GetContext().GetTier().GetName(); n != "" {
		cc.Tier = n
	}
	return cc
}

// setCapCard puts the card on sc where there is a grace popup to answer --
// the free viewer's grace window, sc.GraceDurationSec -- and a page it
// belongs to: not in an embed, where the player has never drawn the lock (no
// transfer status there) and a new upsell on someone else's site is not this
// change's to add.
func (s *ActionScript) setCapCard(sc *StreamContent, c *web.Context, embed bool) {
	if sc.GraceDurationSec == 0 || embed {
		return
	}
	sc.CapCard = NewCapCard(s.i18n, c)
}

// capLent is StreamContent.CapLent: the stream runs on claims an embed's
// registered domain lends its visitors, its owner's (handlers/embed post:
// dsd.Claims replaces the visitor's), so its cap is not the visitor's to
// lift. Read off the claims' source, not off "is an embed": an embed of a
// domain nobody registered runs on the visitor's own.
func capLent(dsd *embed.DomainSettingsData) bool {
	return dsd != nil && dsd.Claims != nil
}
