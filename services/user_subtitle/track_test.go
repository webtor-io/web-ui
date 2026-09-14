package user_subtitle

import (
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

func upload(name string) *models.UserSubtitle {
	return &models.UserSubtitle{
		UserSubtitleID: uuid.NewV4(),
		OriginalName:   name,
		Format:         "srt",
		Size:           2048,
		Hash:           "h-" + name,
	}
}

// Every rendered track carries a language tag, and it comes from the
// filename. The page render and the async reload share this mapper for
// exactly this field: when only the reload derived it, an upload grouped
// under its own language chip until the first F5 and under "Unknown" after.
func TestTracksDerivesSrcLangFromName(t *testing.T) {
	got := Tracks([]*models.UserSubtitle{upload("Coyote.vs.Acme.en.srt"), upload("subtitles.srt")}, nil)

	if len(got) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(got))
	}
	if got[0].SrcLang != "en" {
		t.Errorf("named upload: SrcLang = %q, want \"en\"", got[0].SrcLang)
	}
	if got[1].SrcLang != UndeterminedLang {
		t.Errorf("plain upload: SrcLang = %q, want %q", got[1].SrcLang, UndeterminedLang)
	}
	for _, tr := range got {
		if tr.SrcLang == "" {
			t.Errorf("track %q rendered an empty srclang", tr.OriginalName)
		}
	}
}

// The rest of the shape has to be identical on both paths too — id,
// delete URL and the label the chip prints.
func TestTracksFillsIdentityAndSource(t *testing.T) {
	sub := upload("Movie.ru.srt")
	got := Tracks([]*models.UserSubtitle{sub}, func(s *models.UserSubtitle) string {
		return "https://ext/" + s.Hash
	})

	tr := got[0]
	if tr.ID != TrackID(sub.UserSubtitleID) {
		t.Errorf("ID = %q, want %q", tr.ID, TrackID(sub.UserSubtitleID))
	}
	if tr.DeleteURL != DeleteURL(sub.UserSubtitleID) {
		t.Errorf("DeleteURL = %q, want %q", tr.DeleteURL, DeleteURL(sub.UserSubtitleID))
	}
	if tr.Src != "https://ext/"+sub.Hash {
		t.Errorf("Src = %q, want the wrapped URL", tr.Src)
	}
	if tr.Label != sub.OriginalName || tr.OriginalName != sub.OriginalName {
		t.Errorf("label/name = %q/%q, want %q", tr.Label, tr.OriginalName, sub.OriginalName)
	}
	if tr.Format != sub.Format || tr.Size != sub.Size {
		t.Errorf("format/size = %q/%d, want %q/%d", tr.Format, tr.Size, sub.Format, sub.Size)
	}
}

// A nil wrapper is the "no export URL" case: the list still renders, the
// chips just have nothing to play.
func TestTracksWithoutWrapperLeavesSrcEmpty(t *testing.T) {
	got := Tracks([]*models.UserSubtitle{upload("a.en.srt")}, nil)
	if got[0].Src != "" {
		t.Errorf("Src = %q, want empty", got[0].Src)
	}
}
