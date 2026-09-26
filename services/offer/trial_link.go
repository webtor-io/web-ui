package offer

import (
	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/i18n"
)

// A call to action that starts the promo plan's trial links to /trial
// (handlers/trial), not to the provider's checkout: /trial redirects to that
// same checkout, and on the way the server records the visit — whatever
// blocks analytics in the browser, with the account when it is signed in —
// before the provider, which drops utm parameters, takes over. The link says
// which button it is with ?from=<surface>.
//
// Only the promo plan's trial goes through /trial, because /trial always
// sends to the promo plan. Links to a particular plan (a /donate card of
// another tier, an annual plan, a checkout for a discount code, "manage your
// membership") stay direct.

// The surfaces a /trial link sits on — the closed set of ?from values. The
// counter and the log line take their label from the query string, so
// anything outside this set is counted as "other" (and a visit without the
// parameter — the Stremio clip, a typed webtor.io/trial — as "none").
const (
	FromPromoBanner   = "promo-banner"   // the promo banner (partials/extend.html, deployment-provided)
	FromDownloadNudge = "download-nudge" // download ready (action/download_file.html)
	FromLimitModal    = "limit-modal"    // the speed-cap modal (action/errors/slow_download.html)
	FromGrace         = "grace"          // the grace popup over the player (action/stream_video.html)
	FromNoPeers       = "no-peers"       // no peers, dead swarm: save to Vault (action/errors/no_peers.html)
	FromOnboarding    = "onboarding"     // locked onboarding steps (partials/onboarding_checklist.html)
	FromDonate        = "donate"         // /donate: the promo card's trial plaque and its monthly Join
	FromStatusBar     = "status-bar"     // the plan box under the resource page's transfer status (services/statusview)
)

// TrialFroms lists every known surface, in the order of the table in
// docs/offers.md.
var TrialFroms = []string{
	FromPromoBanner, FromDownloadNudge, FromLimitModal, FromStatusBar, FromGrace, FromNoPeers, FromOnboarding, FromDonate,
}

// Labels for a ?from outside TrialFroms.
const (
	TrialFromNone  = "none"
	TrialFromOther = "other"
)

var knownFroms = func() map[string]bool {
	m := make(map[string]bool, len(TrialFroms))
	for _, f := range TrialFroms {
		m[f] = true
	}
	return m
}()

// TrialFromLabel bounds a client-supplied ?from for a metric label or a log
// field: a known surface as is, "" as "none", anything else as "other".
func TrialFromLabel(from string) string {
	switch {
	case from == "":
		return TrialFromNone
	case knownFroms[from]:
		return from
	}
	return TrialFromOther
}

// TrialPath is the /trial link of a surface, without a language prefix —
// for Go code that hands templates a language-agnostic path (onboarding
// steps), and for a letter, which prefixes the site's domain. from must be
// one of the From* constants.
func TrialPath(from string) string {
	return "/trial?from=" + from
}

// StartsTrial answers "does /trial start a free trial right now": the promo
// plan has a trial the checkout can start. Only then does a surface link to
// /trial; otherwise it keeps its own link (the plan's checkout, /donate) —
// /trial would lead to the same place, and without a catalog it is a 404.
func StartsTrial(o *Offer) bool {
	return o != nil && o.TrialDays > 0
}

// TrialURL is the link of a call to action that starts the promo plan's
// trial: "<lang prefix>/trial?from=<from>" when o (the promo plan as this
// render sees it) has a trial the checkout can start, "" when it has not —
// the template then falls back to the surface's own link. Taking the offer
// from the template keeps the link, the umami target and the "N days free"
// line of one button reading the same snapshot of the catalog.
//
// An unknown from is an error, not "other": the render fails, and so does
// the surface's render test, instead of a typo being counted as nobody.
// Template usage:
//
//	href="{{ or (trialURL $.Lang "grace" $offer) $offer.URL (langPath $.Lang "/donate") }}"
func (h *Helper) TrialURL(lang, from string, o *Offer) (string, error) {
	return TrialURL(lang, from, o)
}

// TrialURL is Helper.TrialURL without the helper, for tests and callers
// outside templates.
func TrialURL(lang, from string, o *Offer) (string, error) {
	if !knownFroms[from] {
		return "", errors.Errorf("trialURL: unknown surface %q (offer.TrialFroms)", from)
	}
	if !StartsTrial(o) {
		return "", nil
	}
	return i18n.LangPath(lang, TrialPath(from)), nil
}
