package user_subtitle

import (
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
)

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
