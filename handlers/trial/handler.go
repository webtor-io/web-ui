// Package trial serves /trial, the one link to the promo plan's free trial.
// It began as the short link printed on the Stremio paywall clip
// (docs/stremio.md, "The paywall clip"): a TV cannot follow a link, so the
// clip shows "webtor.io/trial" and a QR code for a phone. Every button on
// the site that starts the trial links here too, with ?from=<surface>
// (offer.TrialURL, docs/offers.md). All of them go straight to the checkout
// of the plan on sale, with its free trial.
//
// This redirect is also the measurement. The membership provider does not
// hand utm parameters back, so the "trial shortlink" log line and its
// counter are the server's record of which clip or button a trial start
// came from — for the clip, which no analytics script sees, the only one.
package trial

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/metrics"
	"github.com/webtor-io/web-ui/services/offer"
)

// PaywallUTM is the query the Stremio paywall clip's QR code carries after
// /trial (scripts/stremio_paywall_video prints the same string; the clip
// test in handlers/stremio fails when the two part). It is also what the
// /donate fallback gets when a visit brought no utm parameters of its own,
// so a typed "webtor.io/trial" and a scanned one reach /donate labelled
// alike.
const PaywallUTM = "utm_source=stremio&utm_medium=video&utm_campaign=paywall"

var defaultUTM, _ = url.ParseQuery(PaywallUTM)

// promoSource is what the handler asks the offers: what is on sale.
type promoSource interface {
	Promo() *offer.Offer
}

type Handler struct {
	offers promoSource
}

// RegisterHandler mounts /trial. The language prefix (/ru/trial) is stripped
// by the i18n middleware before routing, so the one route serves every
// language. Registered before the resource catch-all (/:resource_id), which
// would otherwise read "trial" as a resource id. Not in the sitemap, and
// noindex like every page the sitemap does not list (web.NoindexDefault).
func RegisterHandler(r *gin.Engine, offers *offer.Service) {
	h := &Handler{offers: offers}
	r.GET("/trial", h.trial)
}

func (h *Handler) trial(c *gin.Context) {
	var o *offer.Offer
	if h.offers != nil {
		o = h.offers.Promo()
	}
	lang := i18n.GetLang(c)
	q := c.Request.URL.Query()
	to, target := Target(o, lang, q)
	from := q.Get("from")

	f := log.Fields{
		"target":       target,
		"lang":         lang,
		"from":         offer.TrialFromLabel(from),
		"utm_source":   q.Get("utm_source"),
		"utm_medium":   q.Get("utm_medium"),
		"utm_campaign": q.Get("utm_campaign"),
	}
	// An unknown surface is kept verbatim (cut short) in the log only: it is
	// how a link somebody placed by hand — a post, a listing — shows up
	// before it earns a name in offer.TrialFroms.
	if f["from"] == offer.TrialFromOther {
		f["from_raw"] = truncate(from, maxRawFrom)
	}
	// The phone that scanned the code is often signed in, and a click on the
	// site more often still; when it is, the visit can be joined to the
	// account like the clip view was.
	if u := auth.GetUserFromContext(c); u != nil && u.HasAuth() {
		f["user_hash"] = auth.LogHash(u.ID)
	}
	log.WithFields(f).Info("trial shortlink")
	metrics.TrialShortlink(target, q.Get("utm_campaign"), from)

	if to == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Redirect(http.StatusFound, to)
}

// Target is where /trial sends a visit, and which kind of place that is
// (metrics.TrialTarget*):
//
//   - the plan's own checkout (Offer.URL — the free-trial checkout when the
//     plan has a trial the provider can start). The visit's utm parameters
//     are not passed on: the provider drops them;
//   - /donate in the visit's language, when the plan cannot be bought
//     directly (no checkout link). There the utm parameters do count, so the
//     visit's own are kept and, for a visit without ?from (the clip, or
//     "webtor.io/trial" typed in), the clip's fill in the ones it lacks. A
//     link from a page of the site (?from=…) is not the clip and gets only
//     its own;
//   - nowhere ("", 404) when nothing is on sale — a deployment without a
//     storefront has no trial to point at.
func Target(o *offer.Offer, lang string, q url.Values) (string, string) {
	if o == nil {
		return "", metrics.TrialTargetNone
	}
	if o.URL != "" {
		return o.URL, metrics.TrialTargetCheckout
	}
	v := url.Values{}
	if q.Get("from") == "" {
		for k, vs := range defaultUTM {
			v[k] = vs
		}
	}
	for k, vs := range q {
		if strings.HasPrefix(k, "utm_") && len(vs) > 0 && vs[0] != "" {
			v[k] = vs[:1]
		}
	}
	to := i18n.LangPath(lang, "/donate")
	if len(v) > 0 {
		to += "?" + v.Encode()
	}
	return to, metrics.TrialTargetDonate
}

// maxRawFrom bounds an unknown ?from in the log line, in bytes.
const maxRawFrom = 64

// truncate cuts s to at most n bytes without splitting a character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
