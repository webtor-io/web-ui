package scripts

import (
	"os"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/web"
)

// The card the player draws by itself once the viewer has answered the grace
// popup: the viewer's cap in the lock's own words, the cap alone as the line
// the file's bitrate would complete, the status's props -- and only where
// there is a popup to answer, on the page (not in an embed), with a cap.
func TestCapCard(t *testing.T) {
	svc := i18n.New(os.DirFS("../../locales"))
	s := &ActionScript{i18n: svc}
	ctx := func(rate, role, lang string) *web.Context {
		return &web.Context{Lang: lang, ApiClaims: &api.Claims{Rate: rate, Role: role}}
	}

	cc := NewCapCard(svc, ctx("5M", "", "ru"))
	if cc == nil {
		t.Fatal("anonymous at 5M: no card")
	}
	want := CapCard{Rate: "5\u00a0Мбит/с", CapMbps: 5, Line: "Без подписки — до 5\u00a0Мбит/с", Auth: "anon", Tier: "free"}
	if *cc != want {
		t.Errorf("anonymous: %+v, want %+v", *cc, want)
	}
	signedIn := ctx("5M", "free", "en")
	signedIn.User = &auth.User{ID: uuid.NewV4()}
	if cc := NewCapCard(svc, signedIn); cc == nil || cc.Auth != "user" || cc.Rate != "5\u00a0Mbps" || cc.Line != "Without a subscription — up to 5\u00a0Mbps" {
		t.Errorf("signed in: %+v", cc)
	}
	for name, c := range map[string]*web.Context{
		"no cap":    ctx("", "", "ru"),
		"no claims": {Lang: "ru"},
		"nil":       nil,
	} {
		if cc := NewCapCard(svc, c); cc != nil {
			t.Errorf("%s: %+v, want none -- no number for the lock", name, cc)
		}
	}
	if NewCapCard(nil, ctx("5M", "", "ru")) != nil {
		t.Error("no i18n: no card")
	}

	// Something faster on sale -- the status box's own test.
	for _, c := range []struct {
		name string
		o    *offer.Offer
		want bool
	}{
		{"50 over 5", &offer.Offer{RateMbps: 50, TrialDays: 7}, true},
		{"unlimited", &offer.Offer{}, true},
		{"as fast as the cap", &offer.Offer{RateMbps: 5}, false},
		{"slower", &offer.Offer{RateMbps: 3}, false},
		{"nothing on sale", nil, false},
	} {
		if got := cc.Sells(c.o); got != c.want {
			t.Errorf("Sells, %s: %v", c.name, got)
		}
	}
	if (*CapCard)(nil).Sells(&offer.Offer{RateMbps: 50}) {
		t.Error("no card sells nothing")
	}

	// Only with a grace popup to answer, and not in an embed.
	for _, c := range []struct {
		name  string
		grace int
		embed bool
		want  bool
	}{
		{"free, grace, the page", 1200, false, true},
		{"no grace window (paid tiers, grace off)", 0, false, false},
		{"an embed", 1200, true, false},
	} {
		sc := &StreamContent{GraceDurationSec: c.grace}
		s.setCapCard(sc, ctx("5M", "", "ru"), c.embed)
		if (sc.CapCard != nil) != c.want {
			t.Errorf("%s: %+v", c.name, sc.CapCard)
		}
	}
}
