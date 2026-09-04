package user_subtitle

import (
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
)

// Every rendered track must carry a language tag: HTML requires srclang on
// kind="subtitles", and an empty attribute is not a valid tag. The language
// comes from the filename when it declares one.
func TestBuildViewDerivesSrcLang(t *testing.T) {
	named, plain := sub(t, "Coyote.vs.Acme.en.srt"), sub(t, "subtitles.srt")
	v := buildView([]*models.UserSubtitle{named, plain}, "res", "/movie.mkv", "http://ei", "", "", nil)

	if got := v.UserSubtitles[0].SrcLang; got != "en" {
		t.Errorf("named upload: SrcLang = %q, want \"en\"", got)
	}
	if got := v.UserSubtitles[1].SrcLang; got != us.UndeterminedLang {
		t.Errorf("plain upload: SrcLang = %q, want %q", got, us.UndeterminedLang)
	}
	for _, tr := range v.UserSubtitles {
		if tr.SrcLang == "" {
			t.Errorf("track %q rendered an empty srclang", tr.OriginalName)
		}
	}
}

func sub(t *testing.T, name string) *models.UserSubtitle {
	t.Helper()
	id := uuid.NewV4()
	return &models.UserSubtitle{
		UserSubtitleID: id,
		OriginalName:   name,
		Format:         "srt",
		Size:           1024,
		Hash:           "h-" + name,
	}
}

// A subtitle the viewer just uploaded has to come back marked, so the player
// can switch to it without a second deliberate click. Uploading and then
// seeing nothing on screen is the whole of support ticket fd718d99.
func TestBuildViewMarksJustUploaded(t *testing.T) {
	a, b := sub(t, "old.srt"), sub(t, "new.srt")
	v := buildView([]*models.UserSubtitle{a, b}, "res", "/movie.mkv", "http://ei", "", us.TrackID(b.UserSubtitleID), nil)

	if len(v.UserSubtitles) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(v.UserSubtitles))
	}
	for _, tr := range v.UserSubtitles {
		want := tr.ID == us.TrackID(b.UserSubtitleID)
		if tr.Selected != want {
			t.Errorf("track %q: Selected = %v, want %v", tr.OriginalName, tr.Selected, want)
		}
	}
}

// Negative control: a plain reload (list after delete, or the initial render)
// selects nothing — otherwise every re-render would yank the viewer's current
// choice over to an arbitrary track.
func TestBuildViewSelectsNothingWithoutUpload(t *testing.T) {
	a, b := sub(t, "one.srt"), sub(t, "two.srt")
	v := buildView([]*models.UserSubtitle{a, b}, "res", "/movie.mkv", "http://ei", "", "", nil)

	for _, tr := range v.UserSubtitles {
		if tr.Selected {
			t.Errorf("track %q must not be selected", tr.OriginalName)
		}
	}
}
