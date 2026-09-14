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
	o := buildSubtitleOpts(paid, true, false, false, false, "pt", []string{"A"})
	if !o.Translate || !o.Paid || o.PreferredLang != "pt" || len(o.Names) != 1 {
		t.Fatalf("%+v", o)
	}
	o = buildSubtitleOpts(&web.Context{}, true, true, false, false, "pt", nil)
	if !o.Paid {
		t.Fatal("free-for-all flag makes everyone paid")
	}
	o = buildSubtitleOpts(paid, true, true, true, false, "pt", nil)
	if o.Translate {
		t.Fatal("adult resource → no translation even when enabled and free for all")
	}
	o = buildSubtitleOpts(paid, false, false, false, false, "pt", nil)
	if o.Translate {
		t.Fatal("feature flag off → no translation")
	}
	if o.PreferredLang != "pt" {
		t.Fatal("preferred language still drives the ladder when translation is off")
	}
	o = buildSubtitleOpts(paid, true, true, false, true, "pt", nil)
	if o.Translate {
		t.Fatal("embed widget → no AI track in phase 2, even for a paid viewer")
	}
	if o.PreferredLang != "pt" || !o.Paid {
		t.Fatalf("the embed only switches translation off: %+v", o)
	}
}

// TestSubtitleOptsForMasterSwitch pins the rule that makes the feature flag
// (and the embed widget) a true master switch rather than a gate on the AI
// item alone: with either of them in force the page must render exactly as
// phase 1 did. PreferredLang is what decides that -- GetSubtitles takes the
// legacy selectListItem path when it is empty and runs applyLadder when it
// is not -- so a non-empty PreferredLang with the flag off would still
// change the default track, the forced-track rule and the audio rule for
// every viewer on a deployment that never switched the feature on.
func TestSubtitleOptsForMasterSwitch(t *testing.T) {
	paid := &web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 2}}}}

	o := subtitleOptsFor(false, false, paid, false, false, "pt", []string{"A"})
	if o.PreferredLang != "" || o.Translate {
		t.Fatalf("feature flag off ⇒ phase-1 selection, no ladder input: %+v", o)
	}
	o = subtitleOptsFor(true, true, paid, false, false, "pt", []string{"A"})
	if o.PreferredLang != "" || o.Translate {
		t.Fatalf("embed ⇒ phase-1 selection, no ladder input: %+v", o)
	}
	o = subtitleOptsFor(true, false, paid, false, false, "pt", []string{"A"})
	if o.PreferredLang != "pt" || !o.Translate || !o.Paid || len(o.Names) != 1 {
		t.Fatalf("enabled and not an embed ⇒ the full ladder input: %+v", o)
	}
	// The NSFW gate still only removes the AI item: the ladder itself keeps
	// running on the preferred language.
	o = subtitleOptsFor(true, false, paid, false, true, "pt", nil)
	if o.PreferredLang != "pt" || o.Translate {
		t.Fatalf("adult ⇒ ladder without the AI item: %+v", o)
	}
}
