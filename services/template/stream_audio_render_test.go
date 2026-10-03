package template_test

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
)

// The audio player carries the stream job's word on the file against the
// viewer's cap as the video player does (stream_video_render_test.go): the
// transfer status reads it off whichever element plays
// (lib/playerActivity.js). Each attribute only with its flag.
func TestStreamAudioCarriesTheStatusMarks(t *testing.T) {
	tpl, err := template.New("stream_audio.html").Funcs(template.FuncMap{
		"getDurationSec": func(interface{}) string { return "60" },
		"json":           func(v interface{}) template.JS { return template.JS("{}") },
		"asset":          func(p string) template.HTML { return template.HTML(p) },
	}).ParseFiles("../../templates/views/action/stream_audio.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	player := func(sc *scripts.StreamContent) string {
		t.Helper()
		var b bytes.Buffer
		if err := tpl.ExecuteTemplate(&b, "main", map[string]interface{}{"Data": sc}); err != nil {
			t.Fatalf("render: %v", err)
		}
		s := b.String()
		i := strings.Index(s, `<audio class="player"`)
		if i < 0 {
			t.Fatal("no player element")
		}
		return s[i : i+strings.Index(s[i:], ">")]
	}
	sc := &scripts.StreamContent{
		ExportTag:           &ra.ExportTag{},
		Item:                &ra.ListItem{PathStr: "track.flac"},
		VideoStreamUserData: &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:            &models.StreamSettings{},
	}
	if tag := player(sc); strings.Contains(tag, "data-status-") {
		t.Errorf("nothing known, yet marked: %s", tag)
	}
	sc.StatusStallSub, sc.StatusOverCap = "up to 5, needs 8", true
	if tag := player(sc); !strings.Contains(tag, ` data-status-stall-sub="up to 5, needs 8"`) || !strings.Contains(tag, " data-status-over-cap") || strings.Contains(tag, "data-status-fits-cap") {
		t.Errorf("over the cap, not marked so: %s", tag)
	}
	sc.StatusOverCap, sc.StatusFitsCap = false, true
	if tag := player(sc); !strings.Contains(tag, " data-status-fits-cap") || strings.Contains(tag, "data-status-over-cap") {
		t.Errorf("under the cap, not marked so: %s", tag)
	}
}
