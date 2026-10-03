// package scripts_test, not scripts: it holds startAudio to the picker's
// own default, handlers/action.Helper.GetAudioTracks, and handlers/action
// imports this package.
package scripts_test

import (
	"encoding/json"
	"testing"

	"golang.org/x/text/language"

	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

// The status's estimate counts the audio track the player starts on, which
// is the one the picker marks default; startAudio is a copy of that rule
// (the picker's package imports this one), so the two are held to one
// answer over saved choices, carried ones and languages.
func TestStartAudio_IsThePickersDefault(t *testing.T) {
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":[
		{"codec_type":"video","codec_name":"h264"},
		{"codec_type":"audio","codec_name":"ac3","channels":6,"channel_layout":"5.1(side)","tags":{"language":"rus"}},
		{"codec_type":"audio","codec_name":"aac","channels":2,"channel_layout":"stereo","tags":{"language":"eng","title":"Commentary"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng"}},
		{"codec_type":"audio","codec_name":"eac3","channels":6,"channel_layout":"5.1","tags":{"language":"eng","title":"Main"}},
		{"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"und"}},
		{"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"pt-BR"}},
		{"codec_type":"audio","codec_name":"mp3","channels":2},
		{"codec_type":"audio","codec_name":"aac","channels":2,"channel_layout":"stereo","tags":{"language":"fre","title":"Director"}},
		{"codec_type":"audio","codec_name":"ac3","channels":6,"channel_layout":"5.1","tags":{"language":"fre"}}]}`), &mp); err != nil {
		t.Fatal(err)
	}
	tags := func(ts ...string) []language.Tag {
		var out []language.Tag
		for _, s := range ts {
			out = append(out, language.MustParse(s))
		}
		return out
	}
	viewers := map[string]*models.VideoStreamUserData{
		"nothing":                     {},
		"the English fallback":        {FallbackLangTag: language.English},
		"English browser":             {AcceptLangTags: tags("en-US", "en"), FallbackLangTag: language.English},
		"Russian browser":             {AcceptLangTags: tags("ru")},
		"Portuguese browser":          {AcceptLangTags: tags("pt")},
		"German browser, no match":    {AcceptLangTags: tags("de"), FallbackLangTag: language.German},
		"Russian picked over English": {AcceptLangTags: tags("en"), ResolvedLang: "ru"},
		"an unreadable picked one":    {AcceptLangTags: tags("ru"), ResolvedLang: "x!"},
		"saved third":                 {AcceptLangTags: tags("ru"), AudioID: "mp-2"},
		"saved, gone from this file":  {AcceptLangTags: tags("ru"), AudioID: "mp-9"},
		"carried English":             {AcceptLangTags: tags("ru"), AudioID: "mp-0", Carry: &models.TrackCarry{AudioLang: "en"}},
		"carried English 5.1":         {AudioID: "mp-0", Carry: &models.TrackCarry{AudioLang: "en", AudioLabel: "Main (5.1)"}},
		"carried French 5.1":          {Carry: &models.TrackCarry{AudioLang: "fr", AudioLabel: "Audio (5.1) #8"}},
		"carried commentary":          {Carry: &models.TrackCarry{AudioLang: "EN", AudioLabel: "Commentary (stereo)"}},
		"carried, no such language":   {AudioID: "mp-1", Carry: &models.TrackCarry{AudioLang: "fr"}},
		"carried, no language":        {AcceptLangTags: tags("pt-BR"), Carry: &models.TrackCarry{AudioLabel: "x"}},
	}
	h := action.NewHelper()
	for name, ud := range viewers {
		want := -1
		for i, li := range h.GetAudioTracks(ud, &mp) {
			if li.Default {
				want = i
			}
		}
		if got := scripts.StartAudio(&mp, ud); got != want {
			t.Errorf("%s: track %d, the picker's default is %d", name, got, want)
		}
	}
	if got := scripts.StartAudio(&mp, nil); got != 0 {
		t.Errorf("no viewer data: %d, want the first", got)
	}
}
