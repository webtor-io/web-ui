package action

import (
	"testing"

	"github.com/webtor-io/web-ui/models"
)

func TestCarryAudioID(t *testing.T) {
	lis := []ListItem{
		{ID: "mp-0", SrcLang: "ru", Label: "Dub (5.1)"},
		{ID: "mp-1", SrcLang: "en", Label: "Original (5.1)"},
		{ID: "mp-2", SrcLang: "en", Label: "Commentary (stereo)"},
	}
	cases := []struct {
		name  string
		carry *models.TrackCarry
		want  string
	}{
		{"no carry", nil, ""},
		{"language only: the first track in it", &models.TrackCarry{AudioLang: "en"}, "mp-1"},
		{"the label tells two English tracks apart", &models.TrackCarry{AudioLang: "en", AudioLabel: "Commentary (stereo)"}, "mp-2"},
		{"a label this file does not have falls back to the language", &models.TrackCarry{AudioLang: "en", AudioLabel: "Director (mono)"}, "mp-1"},
		{"case does not matter", &models.TrackCarry{AudioLang: "RU"}, "mp-0"},
		{"a language this file does not have: nothing, the ladder decides", &models.TrackCarry{AudioLang: "fr"}, ""},
		{"a label never matches across languages", &models.TrackCarry{AudioLang: "fr", AudioLabel: "Original (5.1)"}, ""},
	}
	for _, c := range cases {
		if got := carryAudioID(lis, c.carry); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCarrySubtitleID(t *testing.T) {
	lis := []ListItem{
		{ID: "none"},
		{ID: "mp-3", SrcLang: "en", Provider: "MediaProbe"},
		{ID: "os-1", SrcLang: "ru", Provider: "OpenSubtitles"},
		{ID: "os-2", SrcLang: "en", Provider: "OpenSubtitles"},
		{ID: "ai-kk", SrcLang: "kk", Provider: "Translated"},
		{ID: "ai-sr", SrcLang: "sr", Provider: "Translated", Locked: true},
	}
	cases := []struct {
		name  string
		carry *models.TrackCarry
		want  string
	}{
		{"no carry", nil, ""},
		{"says nothing about subtitles", &models.TrackCarry{AudioLang: "en"}, ""},
		{"off stays off", &models.TrackCarry{Subtitles: "off"}, "none"},
		{"same language, same origin", &models.TrackCarry{Subtitles: "on", SubtitleLang: "en", SubtitleProvider: "OpenSubtitles"}, "os-2"},
		{"same language, origin missing here: any origin", &models.TrackCarry{Subtitles: "on", SubtitleLang: "ru", SubtitleProvider: "MediaProbe"}, "os-1"},
		{"an AI translation is carried as one", &models.TrackCarry{Subtitles: "on", SubtitleLang: "kk", SubtitleProvider: "Translated"}, "ai-kk"},
		{"a locked item is never what a carry selects", &models.TrackCarry{Subtitles: "on", SubtitleLang: "sr", SubtitleProvider: "Translated"}, ""},
		{"a language this file has nothing in", &models.TrackCarry{Subtitles: "on", SubtitleLang: "de"}, ""},
		{"on without a language is not an intent", &models.TrackCarry{Subtitles: "on"}, ""},
		{"an unknown state is ignored", &models.TrackCarry{Subtitles: "maybe", SubtitleLang: "en"}, ""},
	}
	for _, c := range cases {
		if got := carrySubtitleID(lis, c.carry); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWithCarriedSubtitleCopies(t *testing.T) {
	lis := []ListItem{{ID: "none"}, {ID: "os-1", SrcLang: "ru"}}
	ud := &models.VideoStreamUserData{SubtitleID: "mp-9", Carry: &models.TrackCarry{Subtitles: "on", SubtitleLang: "ru"}}
	got := withCarriedSubtitle(ud, lis)
	if got.SubtitleID != "os-1" {
		t.Fatalf("the carry outranks the file's own saved choice, got %q", got.SubtitleID)
	}
	if ud.SubtitleID != "mp-9" {
		t.Fatal("the shared user data must not be written to")
	}
	plain := &models.VideoStreamUserData{SubtitleID: "mp-9"}
	if withCarriedSubtitle(plain, lis) != plain {
		t.Fatal("no carry: the same value comes back")
	}
}

func TestCarryFromForm(t *testing.T) {
	form := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c := carryFromForm(form(map[string]string{})); c != nil {
		t.Fatalf("an ordinary start carries nothing, got %+v", c)
	}
	c := carryFromForm(form(map[string]string{"carry-audio-lang": " en ", "carry-sub": "off", "carry-sub-lang": "ru"}))
	if c == nil || c.AudioLang != "en" || c.Subtitles != "off" || c.SubtitleLang != "ru" {
		t.Fatalf("got %+v", c)
	}
	if c := carryFromForm(form(map[string]string{"carry-sub": "bogus"})); c != nil {
		t.Fatalf("an unknown subtitle state is not a carry, got %+v", c)
	}
}
