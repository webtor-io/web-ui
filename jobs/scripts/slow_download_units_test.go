package scripts

import (
	"html/template"
	"os"
	"testing"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/web"
)

// The cap modal and the transfer status speak of one stream in one unit, the
// megabit thp's limiter caps in (2^20 bits): at a 5M cap the owner's 1080p
// file reads "5 -> 8,7" in the modal, and the status's line under the player
// says "up to 5, and this file needs 8,7" of the same stream. Until
// 2026-09-29 the modal said "5.0 -> 9.1": 10^6 bits, and '.' in every
// language.
func TestCapModal_SaysWhatTheStatusSays(t *testing.T) {
	s := &ActionScript{i18n: i18n.New(os.DirFS("../../locales"))}
	bps := capGateBitrate(probeJSON(t, probeOwner), true)
	tpl, err := template.New("slow_download.html").Funcs(offerFuncs(t, prodCatalog(), true)).
		ParseFiles("../../templates/views/action/errors/slow_download.html", "../../templates/partials/icons.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type ctx struct {
		Lang string
		Data *SlowDownloadData
	}
	for _, c := range []struct {
		lang, capN, needN, sub string
		banned                 []string
	}{
		{"ru", "5", "8,7", "Без подписки — до 5 Мбит/с, а файлу нужно 8,7 Мбит/с", []string{`">9.1</div>`, `">5.0</div>`, `">8.7</div>`}},
		{"en", "5", "8.7", "Without a subscription — up to 5 Mbps, and this file needs 8.7 Mbps", []string{`">9.1</div>`, `">5.0</div>`}},
	} {
		wc := &web.Context{Lang: c.lang, ApiClaims: &api.Claims{Rate: "5M"}}
		if got := s.statusStallSub(wc, bps); got != c.sub {
			t.Fatalf("%s: the status's line %q, want %q", c.lang, got, c.sub)
		}
		sdd, limited := checkCachedRateLimit(wc, bps)
		if !limited {
			t.Fatalf("%s: the owner's file at 5M got no cap modal", c.lang)
		}
		if m, r := sdd.MeasuredLabel(c.lang), sdd.RequiredLabel(c.lang); m != c.capN || r != c.needN {
			t.Errorf("%s: the modal says %q -> %q, the status %s -> %s", c.lang, m, r, c.capN, c.needN)
		}
		out := renderModal(t, tpl, c.lang, &ctx{Lang: c.lang, Data: &sdd})
		assertRender(t, c.lang, out,
			[]string{`leading-none">` + c.capN + `</div>`, `leading-none">` + c.needN + `</div>`,
				// Analytics keep their numbers (html/template quotes them
				// in a script), in the same megabit.
				`measured_mbps: "5.0"`, `required_mbps: "8.7"`},
			c.banned)
	}
}

// Where the cap modal fires at a "5M" cap. thp delivers 5·2^20 = 5,242,880
// bits a second, and the modal fires only for a stream over the cap as the
// labels say them -- exactly where the status marks it over the cap
// (statusview.OverCap). Before 2026-09-29 it took the cap for 5,000,000 and
// fired from 0.954 of what thp delivers.
//
// Not firing below the cap is a choice, not "it fits": a stream in
// FitsMargin's band (at the cap, or under it by less than 1.2) is one the
// status calls unknown, and the player may pull more than its estimate --
// the file FitsMargin records pulled 1.16 times it and stalled at 0.869 of
// the cap. The modal stays out of the band because "the file needs more than
// you have" would read "5 -> 4,9" there; an upfront warning for it would need
// wording of its own (checkCachedRateLimit, docs/warmup.md).
func TestCheckCachedRateLimit_TheLimitersMegabit(t *testing.T) {
	c := ctxWith("5M", "free")
	for _, tc := range []struct {
		name    string
		bps     int64
		limited bool
	}{
		{"the stalled file of FitsMargin, 0.869 of the cap: no modal then or now", 4_556_000, false},
		{"over 5·10^6, under what thp delivers: the old modal's slice of the band", 5_100_000, false},
		{"exactly what thp delivers", 5 << 20, false},
		{"over it by less than the labels show: would read 5 -> 5", 5_260_000, false},
		{"over it as the labels say: 5 -> 5,1", 5_300_000, true},
	} {
		sdd, limited := checkCachedRateLimit(c, tc.bps)
		if limited != tc.limited {
			t.Errorf("%s (%d b/s): cap modal %v, want %v", tc.name, tc.bps, limited, tc.limited)
		}
		if limited && (sdd.MeasuredLabel("ru") != "5" || sdd.RequiredLabel("ru") != "5,1") {
			t.Errorf("%s: %q -> %q, want 5 -> 5,1", tc.name, sdd.MeasuredLabel("ru"), sdd.RequiredLabel("ru"))
		}
	}
	// The same band at bronze: 20,500,000 b/s is 19.6 of a 20M cap.
	if _, limited := checkCachedRateLimit(ctxWith("20M", "bronze"), 20_500_000); limited {
		t.Error("a 19.6 Mbps stream at a 20M cap got the cap modal")
	}
	// Across FitsMargin's band and past the cap: the modal fires exactly
	// where the status says "over the cap" -- none in the band, where the
	// status says nothing upfront either -- and never shows a need at or
	// under the cap.
	for _, rate := range []string{"5M", "20M", "50M", "100M"} {
		c := ctxWith(rate, "free")
		capBits := int64(statusview.RateBitsPerSec(rate))
		for bps := int64(float64(capBits) / statusview.FitsMargin); bps <= capBits*11/10; bps += capBits / 997 {
			sdd, limited := checkCachedRateLimit(c, bps)
			if over := statusOverCap(c, bps); limited != over {
				t.Fatalf("%s, %d b/s: cap modal %v, the status's over-the-cap mark %v", rate, bps, limited, over)
			}
			if limited && statusview.Quantize(sdd.RequiredSpeedMbps) <= statusview.Quantize(sdd.MeasuredSpeedMbps) {
				t.Fatalf("%s, %d b/s: the modal reads %s -> %s", rate, bps, sdd.MeasuredLabel("en"), sdd.RequiredLabel("en"))
			}
		}
	}
}
