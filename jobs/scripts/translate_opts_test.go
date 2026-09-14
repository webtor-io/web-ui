package scripts

import (
	"testing"

	claimsproto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
)

func TestIsPaidForTranslate(t *testing.T) {
	if isPaidForTranslate(&web.Context{}) {
		t.Fatal("nil claims are not paid")
	}
	if isPaidForTranslate(&web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 0, Name: "free"}}}}) {
		t.Fatal("tier 0 is free")
	}
	if !isPaidForTranslate(&web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 2, Name: "silver"}}}}) {
		t.Fatal("tier 2 is paid")
	}
}

func TestTranslateOptsShape(t *testing.T) {
	paid := &web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 2}}}}
	o := buildSubtitleOpts(paid, true, false, false, "pt", []string{"A"})
	if !o.Translate || !o.Paid || o.PreferredLang != "pt" || len(o.Names) != 1 {
		t.Fatalf("%+v", o)
	}
	o = buildSubtitleOpts(&web.Context{}, true, true, false, "pt", nil)
	if !o.Paid {
		t.Fatal("free-for-all flag makes everyone paid")
	}
	o = buildSubtitleOpts(paid, true, true, true, "pt", nil)
	if o.Translate {
		t.Fatal("adult resource → no translation even when enabled and free for all")
	}
	o = buildSubtitleOpts(paid, false, false, false, "pt", nil)
	if o.Translate {
		t.Fatal("feature flag off → no translation")
	}
	if o.PreferredLang != "pt" {
		t.Fatal("preferred language still drives the ladder when translation is off")
	}
}
