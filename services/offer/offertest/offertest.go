// Package offertest reads the calls to action out of a rendered page, for
// the render tests of the surfaces that sell the promo plan: a button that
// starts the trial must link to /trial with its surface (offer.TrialURL),
// every other one must keep its own link.
package offertest

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/webtor-io/web-ui/services/offer"
)

// CTA is one link of a rendered page.
type CTA struct {
	// Event is data-umami-event, Target data-umami-event-target ("" when
	// the link has none), Href the unescaped href.
	Event, Target, Href string
	// Attrs are all of the tag's attributes, unescaped.
	Attrs map[string]string
}

var (
	anchorRe = regexp.MustCompile(`(?s)<a\b[^>]*>`)
	attrRe   = regexp.MustCompile(`(?s)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)
)

// Links lists the <a> elements of out that carry a data-umami-event, in
// document order.
func Links(out string) []CTA {
	var cs []CTA
	for _, tag := range anchorRe.FindAllString(out, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = html.UnescapeString(m[2])
		}
		if attrs["data-umami-event"] == "" {
			continue
		}
		cs = append(cs, CTA{Event: attrs["data-umami-event"], Target: attrs["data-umami-event-target"], Href: attrs["href"], Attrs: attrs})
	}
	return cs
}

// TrialCTAs are the links whose umami target is "trial".
func TrialCTAs(out string) []CTA {
	var cs []CTA
	for _, c := range Links(out) {
		if c.Target == "trial" {
			cs = append(cs, c)
		}
	}
	return cs
}

var trialPathRe = regexp.MustCompile(`^(/[a-z]{2})?/trial$`)

// TrialFrom is the surface a /trial link names: href must be exactly
// "[/<lang>]/trial?from=<one of offer.TrialFroms>" — on this site, nothing
// else in the query. ok is false for anything else.
func TrialFrom(href string) (from string, ok bool) {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || !trialPathRe.MatchString(u.Path) {
		return "", false
	}
	q := u.Query()
	if len(q) != 1 || len(q["from"]) != 1 {
		return "", false
	}
	from = q.Get("from")
	for _, f := range offer.TrialFroms {
		if f == from {
			return from, true
		}
	}
	return from, false
}

// IsTrialLink reports whether href goes to /trial at all, well-formed or
// not — for asserting that a surface without a trial does not.
func IsTrialLink(href string) bool {
	u, err := url.Parse(href)
	return err == nil && trialPathRe.MatchString(u.Path)
}
