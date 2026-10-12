package template_test

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/stremio"
)

// The player offers an embed code only where the instance serves embeds:
// the page renders #embed then, and Player.jsx shows the Embed button only
// when #embed exists. The code names this instance -- without baseUrl the
// SDK points at webtor.io, which does not serve embeds.
func TestStreamVideoEmbedDialogFollowsTheInstance(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		out := renderStreamVideoForEmbed(t, enabled)
		has := strings.Contains(out, `<dialog class="modal" id="embed">`)
		if has != enabled {
			t.Fatalf("embedEnabled=%v: #embed rendered=%v", enabled, has)
		}
		if enabled && !strings.Contains(out, `data-config='{"baseUrl":"https://tv.example.lan"}'`) {
			t.Errorf("the embed code does not name this instance:\n%s", elementBetween(out, `id="embed"`, `</dialog>`))
		}
	}
}

func elementBetween(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

func renderStreamVideoForEmbed(t *testing.T, enabled bool) string {
	t.Helper()
	helper := action.NewHelper()
	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string { return key }
	echoHTML := func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) }
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
		"domain":             func() string { return "https://tv.example.lan" },
		"embedEnabled":       func() bool { return enabled },
		"langPath":           func(lang, p string) string { return p },
		"json":               func(v interface{}) template.JS { return template.JS("{}") },
		"asset":              func(p string) template.HTML { return template.HTML(p) },
		"hasAuth":            func(interface{}) bool { return false },
		"promoOffer":         func() *offer.Offer { return nil },
		"trialURL":           offer.TrialURL,
		"tn":                 func(lang, key string, n int, args ...any) string { return key },
		"withContext":        func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":                  echo,
		"tp":                 echoVariadic,
		"tpHTML":             echoHTML,
	}
	tpl, err := template.New("stream_video.html").Funcs(funcs).
		ParseFiles("../../templates/views/action/stream_video.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<!--stub-->{{ end }}`); err != nil {
		t.Fatal(err)
	}
	data := &scripts.StreamContent{
		ExportTag:           &ra.ExportTag{},
		Resource:            &ra.ResourceResponse{MagnetURI: "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10"},
		Item:                &ra.ListItem{PathStr: "movie.mkv"},
		EIURL:               "http://ei.example.com",
		VideoStreamUserData: &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:            &models.StreamSettings{},
		ExternalData:        &models.ExternalData{},
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{"Data": data, "Lang": "en"}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
