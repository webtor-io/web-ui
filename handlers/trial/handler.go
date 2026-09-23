// Package trial serves /trial, the short link printed on the Stremio paywall
// clip (docs/stremio.md, "The paywall clip"). A TV cannot follow a link, so
// the clip shows "webtor.io/trial" and a QR code for a phone; both land here
// and go straight to the checkout of the plan on sale, with its free trial.
//
// This redirect is also the measurement. The membership provider does not
// hand utm parameters back, so the "trial shortlink" log line and its
// counter are the only record that a click came from the clip.
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

	f := log.Fields{
		"target":       target,
		"lang":         lang,
		"utm_source":   q.Get("utm_source"),
		"utm_medium":   q.Get("utm_medium"),
		"utm_campaign": q.Get("utm_campaign"),
	}
	// The phone that scanned the code is often signed in; when it is, the
	// visit can be joined to the account like the clip view was.
	if u := auth.GetUserFromContext(c); u != nil && u.HasAuth() {
		f["user_hash"] = auth.LogHash(u.ID)
	}
	log.WithFields(f).Info("trial shortlink")
	metrics.TrialShortlink(target, q.Get("utm_campaign"))

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
//     visit's own are kept and the clip's fill in the ones it lacks;
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
	for k, vs := range defaultUTM {
		v[k] = vs
	}
	for k, vs := range q {
		if strings.HasPrefix(k, "utm_") && len(vs) > 0 && vs[0] != "" {
			v[k] = vs[:1]
		}
	}
	return i18n.LangPath(lang, "/donate") + "?" + v.Encode(), metrics.TrialTargetDonate
}
