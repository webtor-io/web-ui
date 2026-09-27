package template_test

import (
	"bytes"
	"html"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/web"
)

// The card behind the player's lock once the viewer has answered the grace
// popup (owner, 2026-09-27), as the stream job renders it on the <video>
// (data-cap-card-*, StreamContent.CapCard). It stands in for the transfer
// status's own label until that comes, so it must say exactly what the
// status's stream box says for the same viewer and the same promo plan: the
// same number, title, line, button, note and link, in every language.

var (
	videoTagRe = regexp.MustCompile(`(?s)<video\b[^>]*>`)
	tagAttrRe  = regexp.MustCompile(`(?s)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)
)

// capCardTemplate parses stream_video.html with the real i18n helper, the
// real trialURL and the promo plan o (nil: nothing on sale).
func capCardTemplate(t *testing.T, svc *i18n.Service, o *offer.Offer) *template.Template {
	t.Helper()
	helper := action.NewHelper()
	h := i18n.NewHelper(svc)
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
		"withContext":        func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":                  h.T,
		"tp":                 h.Tp,
		"tn":                 h.Tn,
		"tpHTML":             h.TpHTML,
	}
	tpl, err := template.New("stream_video.html").Funcs(funcs).ParseFiles("../../templates/views/action/stream_video.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<!--stub-->{{ end }}`); err != nil {
		t.Fatal(err)
	}
	return tpl
}

// graceStream is a free viewer's stream with its grace window, as the job
// fills it: the card for the viewer (nil for none).
func graceStream(card *scripts.CapCard) *scripts.StreamContent {
	return &scripts.StreamContent{
		ExportTag:           &ra.ExportTag{},
		Resource:            &ra.ResourceResponse{},
		Item:                &ra.ListItem{PathStr: "movie.mkv"},
		Title:               "Movie",
		EIURL:               "http://ei.example.com",
		VideoStreamUserData: &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:            &models.StreamSettings{},
		ExternalData:        &models.ExternalData{},
		GraceDurationSec:    1200,
		GraceFreeRateMbps:   5,
		CapCard:             card,
	}
}

// renderVideo renders the stream and returns the whole output and the
// player's <video> attributes, unescaped.
func renderVideo(t *testing.T, tpl *template.Template, lang string, data *scripts.StreamContent) (string, map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{"Data": data, "Lang": lang, "User": nil}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	tag := videoTagRe.FindString(out)
	if tag == "" {
		t.Fatalf("no <video> in:\n%s", out)
	}
	attrs := map[string]string{}
	for _, m := range tagAttrRe.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(m[1])] = html.UnescapeString(m[2])
	}
	return out, attrs
}

// staticOffers is a storefront whose promo plan is o.
type staticOffers struct{ o *offer.Offer }

func (s staticOffers) Promo() *offer.Offer         { return s.o }
func (s staticOffers) FasterOnSale(r float64) bool { return false }

func TestCapCardSaysWhatTheStatusBoxSays(t *testing.T) {
	svc := i18n.New(os.DirFS("../../locales"))
	for _, oc := range offerCases {
		if oc.o == nil {
			continue // nothing on sale: TestCapCardAbsent
		}
		tpl := capCardTemplate(t, svc, oc.o)
		for _, lang := range i18n.SupportedLangs {
			for _, signedIn := range []bool{false, true} {
				c := &web.Context{Lang: lang, ApiClaims: &api.Claims{Rate: "5M"}}
				if signedIn {
					c.User = &auth.User{ID: uuid.NewV4()}
				}
				_, attrs := renderVideo(t, tpl, lang, graceStream(scripts.NewCapCard(svc, c)))

				// The status for the same viewer at the cap, the box due, the
				// file's bitrate unknown to it (its line is the cap alone, as
				// the card's own when the job has no probe).
				v := statusview.Build(statusview.Input{
					Lang: lang, Loc: svc.Localizer(lang),
					Torrent:      statusview.Torrent{State: "cached", Progress: 100},
					Viewer:       statusview.Viewer{Known: true, Present: true, Mbps: 5, Limited: true, PlanBox: true, CapMbps: 5},
					SignedIn:     signedIn,
					ClaimCapMbps: 5,
					Offers:       staticOffers{oc.o},
				})
				if v.Plan == nil || v.Plan.Stream.Box == nil {
					t.Fatalf("%s/%s: fixture: the status has no stream box", oc.name, lang)
				}
				box := v.Plan.Stream.Box
				name := oc.name + "/" + lang
				for attr, want := range map[string]string{
					"data-cap-card-rate":   v.Plan.Player.Rate,
					"data-cap-card-title":  box.Title,
					"data-cap-card-sub":    box.Sub,
					"data-cap-card-cta":    box.CTA.Label,
					"data-cap-card-url":    v.Plan.Player.URL,
					"data-cap-card-target": box.CTA.Target,
					"data-cap-card-auth":   v.Auth,
					"data-cap-card-tier":   v.Tier,
				} {
					if got, ok := attrs[attr]; !ok || got != want || want == "" {
						t.Errorf("%s: %s = %q (present %v), the status says %q", name, attr, got, ok, want)
					}
				}
				if note, ok := attrs["data-cap-card-note"]; note != box.CTA.Note || ok != (box.CTA.Note != "") {
					t.Errorf("%s: note %q (present %v), the status says %q", name, note, ok, box.CTA.Note)
				}
				if oc.target == "trial" {
					if want := i18n.LangPath(lang, "/trial?from=player-label"); attrs["data-cap-card-url"] != want {
						t.Errorf("%s: the player's own surface: %q, want %q", name, attrs["data-cap-card-url"], want)
					}
				}
				for attr, val := range attrs {
					if strings.HasPrefix(attr, "data-cap-card-") && (strings.Contains(val, "offer.") || strings.Contains(val, "resource.status.")) {
						t.Errorf("%s: %s is an unresolved key: %q", name, attr, val)
					}
				}
			}
		}
	}
}

// Where there is nothing to draw the card with -- no grace popup to answer
// (a paying viewer, grace off: the job sets no card), nothing on sale, or a
// promo plan no faster than the cap -- the <video> carries none of it, and
// is the tag it was.
func TestCapCardAbsent(t *testing.T) {
	svc := i18n.New(os.DirFS("../../locales"))
	card := scripts.NewCapCard(svc, &web.Context{Lang: "en", ApiClaims: &api.Claims{Rate: "5M"}})
	slow := &offer.Offer{Tier: "bronze", PeriodDays: 30, RateMbps: 5, TrialDays: 7}
	for _, c := range []struct {
		name string
		o    *offer.Offer
		card *scripts.CapCard
	}{
		{"no card (paid tier, no grace)", trialOffer, nil},
		{"nothing on sale", nil, card},
		{"the promo plan no faster than the cap", slow, card},
	} {
		out, attrs := renderVideo(t, capCardTemplate(t, svc, c.o), "en", graceStream(c.card))
		if strings.Contains(out, "data-cap-card") {
			t.Errorf("%s: %v", c.name, attrs)
		}
	}
	// And with one, the tag only gains the card: nothing else moves.
	withCard := graceStream(card)
	_, with := renderVideo(t, capCardTemplate(t, svc, trialOffer), "en", withCard)
	_, without := renderVideo(t, capCardTemplate(t, svc, trialOffer), "en", graceStream(nil))
	for k, v := range without {
		if with[k] != v {
			t.Errorf("%s: %q with the card, %q without", k, with[k], v)
		}
	}
	if n := len(with) - len(without); n != 9 {
		t.Errorf("the card adds %d attributes, want 9: %v", n, with)
	}
}

// Everything on the tag is escaped by the template: a line or a number with
// quotes and markup reads back as it was, and never closes the attribute.
func TestCapCardEscaped(t *testing.T) {
	svc := i18n.New(os.DirFS("../../locales"))
	card := &scripts.CapCard{Rate: `5 "Mbps"`, CapMbps: 5, Line: `up to <b>5</b> & "more"`, Auth: `anon" onload="x`, Tier: "free"}
	out, attrs := renderVideo(t, capCardTemplate(t, svc, trialOffer), "en", graceStream(card))
	for attr, want := range map[string]string{
		"data-cap-card-rate": card.Rate,
		"data-cap-card-sub":  card.Line,
		"data-cap-card-auth": card.Auth,
	} {
		if attrs[attr] != want {
			t.Errorf("%s = %q, want %q", attr, attrs[attr], want)
		}
	}
	if _, ok := attrs["onload"]; ok || strings.Contains(out, "<b>5</b>") {
		t.Errorf("not escaped:\n%s", videoTagRe.FindString(out))
	}
}

// The player's wiring tests (assets/src/js/lib/player/Player.wiring.test.js)
// run the answer's lock on the <video> this job renders -- a free anonymous
// viewer in Russian, a grace window, the promo plan's trial on sale, a file
// that needs 8.7 Mbps -- not on a hand-written look-alike. Committed, since
// `npm test` needs no Go; compared byte for byte here, and rewritten only by
// capCardRegenCmd.
const capCardFixturePath = "../../assets/src/js/lib/player/__fixtures__/cap-card-video.html"

const capCardRegenCmd = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./services/template/ -run TestCapCardFixtureIsCurrent`

func TestCapCardFixtureIsCurrent(t *testing.T) {
	svc := i18n.New(os.DirFS("../../locales"))
	data := graceStream(scripts.NewCapCard(svc, &web.Context{Lang: "ru", ApiClaims: &api.Claims{Rate: "5M"}}))
	data.StatusStallSub = statusview.StallSub(svc.Localizer("ru"), "ru", false, 5, 8.7)
	out, attrs := renderVideo(t, capCardTemplate(t, svc, trialOffer), "ru", data)
	if attrs["data-cap-card-url"] != "/ru/trial?from=player-label" {
		t.Fatalf("fixture: not the card this is for: %v", attrs)
	}
	got := videoTagRe.FindString(out) + "\n"
	if os.Getenv("UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(capCardFixturePath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(capCardFixturePath)
	if err != nil {
		t.Fatalf("read %s: %v\nregenerate with:\n  %s", capCardFixturePath, err, capCardRegenCmd)
	}
	if string(want) != got {
		t.Fatalf("%s is stale: stream_video.html renders the player's <video> differently now.\n"+
			"Regenerate with:\n  %s\nthen re-run `npm test`.", capCardFixturePath, capCardRegenCmd)
	}
}
