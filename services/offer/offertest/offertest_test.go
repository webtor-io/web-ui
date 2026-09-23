package offertest

import "testing"

func TestTrialFrom(t *testing.T) {
	for href, want := range map[string]string{
		"/trial?from=grace":        "grace",
		"/ru/trial?from=donate":    "donate",
		"/pt/trial?from=no-peers":  "no-peers",
		"/trial?from=onboarding":   "onboarding",
		"/trial?from=promo-banner": "promo-banner",
	} {
		if got, ok := TrialFrom(href); !ok || got != want {
			t.Errorf("TrialFrom(%q) = %q, %v; want %q", href, got, ok, want)
		}
	}
	for _, href := range []string{
		"", "/trial", "/trial?from=", "/trial?from=other", "/trial?from=none", "/trial?from=reddit",
		"/trial?from=grace&from=donate", "/trial?from=grace&utm_campaign=x", "/trial?from=grace#x",
		"https://webtor.io/trial?from=grace", "//evil.example/trial?from=grace", "/donate?from=grace",
		"/ru/trial/x?from=grace", "/rus/trial?from=grace", "https://checkout.example/silver?trial",
	} {
		if got, ok := TrialFrom(href); ok {
			t.Errorf("TrialFrom(%q) = %q, ok; want not a surface link", href, got)
		}
	}
}

func TestLinks(t *testing.T) {
	out := `<a href="/x">plain</a>
<a class="btn" href="/trial?from=grace" target="_blank"
   data-umami-event="donate-grace" data-umami-event-tier="free"
   data-umami-event-target="trial">go</a>
<a data-umami-event="donate-download" href="https://c.example/?rid=1&amp;is_free_trial=true" data-umami-event-target="checkout">x</a>
<a href="/donate" data-umami-event="donate-trial-plaque">plaque</a>`
	got := Links(out)
	want := []struct{ event, target, href string }{
		{"donate-grace", "trial", "/trial?from=grace"},
		{"donate-download", "checkout", "https://c.example/?rid=1&is_free_trial=true"},
		{"donate-trial-plaque", "", "/donate"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i].Event != w.event || got[i].Target != w.target || got[i].Href != w.href {
			t.Errorf("link %d: %+v, want %+v", i, got[i], w)
		}
	}
	if got[0].Attrs["data-umami-event-tier"] != "free" {
		t.Errorf("attrs: %v", got[0].Attrs)
	}
	if tc := TrialCTAs(out); len(tc) != 1 || tc[0].Event != "donate-grace" {
		t.Errorf("TrialCTAs = %+v", tc)
	}
}
