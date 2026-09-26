package template_test

import (
	"bytes"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/offer/offertest"
	"github.com/webtor-io/web-ui/services/stremio"
)

var (
	trialOffer    = &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true, TrialDays: 7, URL: "https://checkout.example/silver?trial"}
	checkoutOffer = &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true, URL: "https://checkout.example/silver"}
	donateOffer   = &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true}
)

// offerCases are the states of the promo plan a trial button has to handle:
// the one CTA the surface renders, and where it must lead.
var offerCases = []struct {
	name   string
	o      *offer.Offer
	target string // "" = no CTA at all
	href   string // for a non-trial target; a path is in the page's language
}{
	{"trial", trialOffer, "trial", ""},
	{"checkout cannot start the trial", checkoutOffer, "checkout", "https://checkout.example/silver"},
	{"no membership provider", donateOffer, "donate", "/donate"},
	{"nothing on sale", nil, "", ""},
}

// checkTrialCTA asserts what a surface rendered for one offer state: with a
// trial, every CTA whose umami target is "trial" goes to /trial in the page's
// language and names the surface; without one, nothing goes to /trial and
// the one CTA keeps the surface's own link.
func checkTrialCTA(t *testing.T, name, lang, from, out, target, href string) {
	t.Helper()
	var ctas []offertest.CTA
	for _, l := range offertest.Links(out) {
		if l.Target != "" {
			ctas = append(ctas, l)
		}
		if target != "trial" && offertest.IsTrialLink(l.Href) {
			t.Errorf("%s/%s: %s links to /trial without a trial to start: %q", name, lang, l.Event, l.Href)
		}
	}
	if target == "" {
		if len(ctas) != 0 {
			t.Errorf("%s/%s: nothing on sale, yet %+v", name, lang, ctas)
		}
		return
	}
	if len(ctas) != 1 {
		t.Errorf("%s/%s: want exactly one CTA, got %+v", name, lang, ctas)
		return
	}
	got := ctas[0]
	if got.Target != target {
		t.Errorf("%s/%s: target %q, want %q", name, lang, got.Target, target)
	}
	if target == "trial" {
		want := i18n.LangPath(lang, offer.TrialPath(from))
		if f, ok := offertest.TrialFrom(got.Href); !ok || f != from || got.Href != want {
			t.Errorf("%s/%s: trial CTA links to %q, want %q", name, lang, got.Href, want)
		}
		return
	}
	if strings.HasPrefix(href, "/") {
		href = i18n.LangPath(lang, href)
	}
	if got.Href != href {
		t.Errorf("%s/%s: href %q, want %q (the surface's own link)", name, lang, got.Href, href)
	}
}

// The grace popup over the player starts the trial through /trial.
func TestGracePopupStartsTheTrialThroughTrial(t *testing.T) {
	helper := action.NewHelper()
	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string { return key }
	for _, c := range offerCases {
		o := c.o
		funcs := template.FuncMap{
			"getSubtitles":       helper.GetSubtitles,
			"getAudioTracks":     helper.GetAudioTracks,
			"hasControls":        helper.HasControls,
			"getDurationSec":     helper.GetDurationSec,
			"userSubtitleView":   helper.UserSubtitleView,
			"subtitleLangGroups": helper.SubtitleLangGroups,
			"stremioLanguages":   func() []stremio.Language { return stremio.Languages },
			"originCode":         helper.OriginCode,
			"originCodeForBadge": helper.OriginCodeForBadge,
			"originKey":          helper.OriginKey,
			"originHintKey":      helper.OriginHintKey,
			"propertyTags":       helper.PropertyTags,
			"audioSuffix":        helper.AudioSuffix,
			"langDisplay":        stremio.NewHelper().LangDisplay,
			"langDisplayIn":      stremio.NewHelper().LangDisplayIn,
			"domain":             func() string { return "https://example.com" },
			"langPath":           i18n.LangPath,
			"json":               func(v interface{}) template.JS { return template.JS("{}") },
			"asset":              func(p string) template.HTML { return template.HTML(p) },
			"hasAuth":            func(interface{}) bool { return false },
			"promoOffer":         func() *offer.Offer { return o },
			"trialURL":           offer.TrialURL,
			"tn":                 func(lang, key string, n int, args ...any) string { return key },
			"withContext":        func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
			"t":                  echo,
			"tp":                 echoVariadic,
			"tpHTML":             func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) },
		}
		tpl, err := template.New("stream_video.html").Funcs(funcs).ParseFiles("../../templates/views/action/stream_video.html")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<!--stub-->{{ end }}`); err != nil {
			t.Fatal(err)
		}
		data := &scripts.StreamContent{
			ExportTag:           &ra.ExportTag{},
			Resource:            &ra.ResourceResponse{},
			Item:                &ra.ListItem{PathStr: "movie.mkv"},
			Title:               "Movie",
			EIURL:               "http://ei.example.com",
			VideoStreamUserData: &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
			Settings:            &models.StreamSettings{},
			ExternalData:        &models.ExternalData{},
			GraceDurationSec:    60,
			GraceFreeRateMbps:   5,
		}
		for _, lang := range []string{"en", "ru", "pt"} {
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{"Data": data, "Lang": lang, "User": nil}); err != nil {
				t.Fatalf("%s: execute: %v", c.name, err)
			}
			out := buf.String()
			if !strings.Contains(out, `id="grace-cta"`) {
				t.Fatalf("%s: the grace popup did not render", c.name)
			}
			checkTrialCTA(t, "grace/"+c.name, lang, offer.FromGrace, out, c.target, c.href)
		}
	}
}

// The promo banner is deployment-provided (partials/extend.html, gitignored
// here and kept with the deployment's values), so this runs where that file
// is present — a developer checkout set up like production — and is skipped
// in a bare clone.
func TestPromoBannerStartsTheTrialThroughTrial(t *testing.T) {
	const extend = "../../templates/partials/extend.html"
	if _, err := os.Stat(extend); err != nil {
		t.Skip("no deployment-provided partials/extend.html")
	}
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	defer locales.Close()
	h := i18n.NewHelper(i18n.New(locales.FS()))
	for _, c := range offerCases {
		o := c.o
		tpl, err := template.New("extend.html").Funcs(template.FuncMap{
			"t":          h.T,
			"tp":         h.Tp,
			"tn":         h.Tn,
			"langPath":   i18n.LangPath,
			"promoOffer": func() *offer.Offer { return o },
			"trialURL":   offer.TrialURL,
		}).ParseFiles(extend)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for _, lang := range i18n.SupportedLangs {
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "promo", map[string]any{"Lang": lang}); err != nil {
				t.Fatalf("%s: execute: %v", c.name, err)
			}
			checkTrialCTA(t, "promo/"+c.name, lang, offer.FromPromoBanner, buf.String(), c.target, c.href)
		}
	}
}

var trialURLCallRe = regexp.MustCompile(`trialURL\s+\$\.(?:Ctx\.)?Lang\s+"([^"]*)"`)

// anchorTags returns the <a …> opening tags of a template's source, each up
// to the first ">" outside a {{ … }} action.
func anchorTags(src string) []string {
	var tags []string
	for i := 0; i < len(src); i++ {
		if !strings.HasPrefix(src[i:], "<a") || i+2 >= len(src) || !strings.ContainsRune(" \t\r\n", rune(src[i+2])) {
			continue
		}
		depth := 0
		for j := i + 2; j < len(src); j++ {
			switch {
			case strings.HasPrefix(src[j:], "{{"):
				depth++
				j++
			case strings.HasPrefix(src[j:], "}}"):
				depth--
				j++
			case src[j] == '>' && depth == 0:
				tags = append(tags, src[i:j+1])
				i = j
				j = len(src)
			}
		}
	}
	return tags
}

// Every template, the deployment-provided extend.html included when it is
// there: a link whose umami target can be "trial" takes its href from
// trialURL, and every trialURL call names a surface of offer.TrialFroms.
// The render tests above and in jobs/scripts, handlers/donate and
// services/onboarding prove the links per surface; this catches the next
// surface that forgets the helper, before anyone writes its test.
func TestTrialTargetsTakeTheirLinkFromTrialURL(t *testing.T) {
	known := map[string]bool{}
	for _, f := range offer.TrialFroms {
		known[f] = true
	}
	used := map[string]bool{}
	err := filepath.WalkDir("../../templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for _, m := range trialURLCallRe.FindAllStringSubmatch(src, -1) {
			used[m[1]] = true
			if !known[m[1]] {
				t.Errorf("%s: trialURL names %q, not a surface of offer.TrialFroms", path, m[1])
			}
		}
		for _, tag := range anchorTags(src) {
			i := strings.Index(tag, "data-umami-event-target=")
			if i < 0 {
				continue
			}
			attr := tag[i:]
			if end := strings.Index(attr[len("data-umami-event-target=")+1:], `"`); end >= 0 {
				attr = attr[:len("data-umami-event-target=")+1+end]
			}
			if !strings.Contains(attr, "trial") {
				continue
			}
			if !regexp.MustCompile(`href="[^"]*trialURL`).MatchString(tag) {
				t.Errorf("%s: a CTA that can start the trial must link through trialURL:\n%s", path, tag)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The public templates call trialURL for every surface but the three
	// that are not a template call: onboarding (a path set in Go,
	// services/onboarding), the status bar (a link built in Go and carried
	// over the status SSE, services/statusview) and
	// the promo banner, whose template the deployment provides.
	for _, f := range offer.TrialFroms {
		if f == offer.FromOnboarding || f == offer.FromStatusBar || f == offer.FromPromoBanner {
			continue
		}
		if !used[f] {
			t.Errorf("surface %q is listed in offer.TrialFroms but no template links it", f)
		}
	}
}
