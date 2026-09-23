package offer

import (
	"strings"
	"testing"
)

// A surface links to /trial only when /trial starts a trial; otherwise it
// keeps its own link, which the template supplies after "".
func TestTrialURL(t *testing.T) {
	trial := &Offer{Tier: "silver", PeriodDays: 30, TrialDays: 7, URL: "https://checkout.example/silver?trial"}
	cases := []struct {
		name, lang, from string
		o                *Offer
		want             string
	}{
		{"english has no prefix", "en", FromGrace, trial, "/trial?from=grace"},
		{"the page's language", "ru", FromDownloadNudge, trial, "/ru/trial?from=download-nudge"},
		{"no language", "", FromDonate, trial, "/trial?from=donate"},
		// The checkout cannot start the trial (TrialDays is 0 then, by
		// offerFor): /trial would lead to the plain checkout — the surface
		// links there itself.
		{"no trial", "en", FromGrace, &Offer{Tier: "silver", URL: "https://checkout.example/silver"}, ""},
		{"no checkout", "en", FromGrace, &Offer{Tier: "silver"}, ""},
		// Nothing on sale: /trial is a 404.
		{"no offer", "en", FromGrace, nil, ""},
	}
	for _, c := range cases {
		got, err := TrialURL(c.lang, c.from, c.o)
		if err != nil || got != c.want {
			t.Errorf("%s: TrialURL(%q, %q) = %q, %v; want %q", c.name, c.lang, c.from, got, err, c.want)
		}
	}
}

// Every surface of the list builds its link, in every state of the offer.
func TestTrialURLKnowsEverySurface(t *testing.T) {
	trial := &Offer{TrialDays: 7, URL: "u"}
	for _, from := range TrialFroms {
		for _, o := range []*Offer{trial, nil} {
			if _, err := TrialURL("en", from, o); err != nil {
				t.Errorf("%s: %v", from, err)
			}
		}
		if TrialFromLabel(from) != from {
			t.Errorf("%s: label %q", from, TrialFromLabel(from))
		}
	}
}

// A surface outside the list fails the render — with or without a trial —
// rather than send clicks nobody can attribute.
func TestTrialURLRejectsAnUnknownSurface(t *testing.T) {
	for _, from := range []string{"", "Grace", "reddit", TrialFromNone, TrialFromOther} {
		for _, o := range []*Offer{{TrialDays: 7, URL: "u"}, nil} {
			if got, err := TrialURL("en", from, o); err == nil || got != "" {
				t.Errorf("from=%q offer=%v: %q, %v; want an error", from, o, got, err)
			}
		}
	}
}

func TestTrialFromLabel(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "none",
		"grace":                  "grace",
		"promo-banner":           "promo-banner",
		"reddit":                 "other",
		"none":                   "other",
		"GRACE":                  "other",
		strings.Repeat("a", 999): "other",
	} {
		if got := TrialFromLabel(in); got != want {
			t.Errorf("TrialFromLabel(%.20q) = %q, want %q", in, got, want)
		}
	}
}

// The values go into a query string and a metric label as they are.
func TestTrialFromsAreURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range TrialFroms {
		if seen[f] {
			t.Errorf("%q listed twice", f)
		}
		seen[f] = true
		if f == TrialFromNone || f == TrialFromOther {
			t.Errorf("%q is a reserved label", f)
		}
		for _, r := range f {
			if (r < 'a' || r > 'z') && r != '-' {
				t.Errorf("%q: only a-z and '-'", f)
				break
			}
		}
	}
}
