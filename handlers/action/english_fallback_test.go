package action

import (
	"os"
	"regexp"
	"testing"

	"golang.org/x/text/language"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

// The English track beside a translation offer (owner, 2026-09-28): when
// the viewer's language has no human track and the audio is not in it, the
// best English track by ladder rank plays while the translation stays an
// offer. offeredDefault in helper.go; the client half is
// pickDefaultSubtitle in subtitle-rules.js.

// osAbs is an OpenSubtitles track with an absolute Src: TranslateURL
// refuses anything else, and a fixture whose only source is refused gets
// no AI item at all -- the offer these tests are about would never exist.
func osAbs(id, lang, source string) api.OpenSubtitleTrack {
	return api.OpenSubtitleTrack{ID: id, Source: source, ExportTrack: &ra.ExportTrack{
		Src: "https://x/os-" + id + ".vtt", SrcLang: lang, Label: lang + " " + id, Kind: "subtitles",
	}}
}

// enViewer is what production hands the ladder: NewVideoStreamUserData
// sets FallbackLangTag to English on every stream.
func enViewer(accept ...language.Tag) *models.VideoStreamUserData {
	return &models.VideoStreamUserData{AcceptLangTags: accept, FallbackLangTag: language.English}
}

var paidPT = SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true}

// TestOfferPlaysTheBestEnglishTrackByLadderRank is the rule itself, and the
// "by ladder rank" half of it: the upload (rank 0) is the last item on the
// list and still wins over the embedded track (rank 1) that list order --
// the phase-1 matcher's tie-break -- would have picked.
//
// Negative controls: with applyLadder's Offered branch back on lis[0] the
// default is "none"; with offeredDefault answering through fallbackIndex
// instead of bestByLadder it is mp-0.
func TestOfferPlaysTheBestEnglishTrackByLadderRank(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}]`)
	oss := []api.OpenSubtitleTrack{osAbs("1", "en", "imdb"), osAbs("2", "en", "hash")}
	user := []models.UserSubtitleTrack{{ID: "us-1", Src: "https://x/mine.vtt", Label: "mine.en.srt", SrcLang: "en"}}

	items := NewHelper().GetSubtitles(enViewer(language.Portuguese), mp, &ra.ExportTag{}, oss, &models.ExternalData{}, user, paidPT)
	tr, ok := byID(items)["tr-pt"]
	if !ok || tr.Locked {
		t.Fatalf("fixture must produce an unlocked AI item, got %+v", tr)
	}
	if !tr.Offered || tr.Default {
		t.Fatalf("the translation stays an offer: %+v", tr)
	}
	if d := defaultID(items); d != "us-1" {
		t.Fatalf("default=%s want us-1: the English track with the best ladder rank", d)
	}
	if byID(items)["us-1"].Saved {
		t.Fatal("the ladder's pick is not the viewer's choice: Saved must stay false")
	}
	if s := suggestedID(t, items); s != "" {
		t.Fatalf("subtitles are on, so nothing is Suggested; got %s", s)
	}

	// Without the upload the next rung down plays: embedded, not either
	// OpenSubtitles track.
	items = NewHelper().GetSubtitles(enViewer(language.Portuguese), mp, &ra.ExportTag{}, oss, &models.ExternalData{}, nil, paidPT)
	if d := defaultID(items); d != "mp-0" {
		t.Fatalf("default=%s want mp-0", d)
	}
}

// TestOfferEnglishIgnoresAcceptLanguage keeps what the 2026-09-18 rule was
// for: the browser's other languages do not decide what plays beside an
// offer. The viewer asked for Portuguese; the Russian track their browser
// also accepts would read as that setting being ignored. English is the
// fallback every viewer gets, and with no English track the answer is
// still "None" -- not the Accept-Language match.
//
// Negative control: offeredDefault falling back to fallbackIndex (the
// phase-1 Accept-Language selection) turns os-1 on in both halves.
func TestOfferEnglishIgnoresAcceptLanguage(t *testing.T) {
	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]`)
	ud := enViewer(language.Portuguese, language.Russian, language.English)

	items := NewHelper().GetSubtitles(ud, audio, &ra.ExportTag{}, []api.OpenSubtitleTrack{osAbs("1", "ru", "hash"), osAbs("2", "en", "imdb")}, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["tr-pt"].Offered {
		t.Fatalf("fixture must offer a translation: %+v", byID(items)["tr-pt"])
	}
	if d := defaultID(items); d != "os-2" {
		t.Fatalf("default=%s want os-2 (English), not the Russian track Accept-Language implies", d)
	}

	items = NewHelper().GetSubtitles(ud, audio, &ra.ExportTag{}, []api.OpenSubtitleTrack{osAbs("1", "ru", "hash")}, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["tr-pt"].Offered {
		t.Fatalf("fixture must offer a translation: %+v", byID(items)["tr-pt"])
	}
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none: no English track, and Accept-Language does not decide beside an offer", d)
	}
}

// TestOfferEnglishNotWhenAudioIsInThePreferredLanguage: the audio rule runs
// first. Portuguese audio for a Portuguese viewer needs no subtitles, and an
// English track on the list does not change that.
func TestOfferEnglishNotWhenAudioIsInThePreferredLanguage(t *testing.T) {
	tag, oss := humanTracks()
	items := NewHelper().GetSubtitles(enViewer(language.Portuguese), audioProbe("por"), tag, oss, &models.ExternalData{}, nil, paidPT)
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none: the audio is already in the viewer's language", d)
	}
}

// TestFreeViewerKeepsThePhaseOneFallback: the rule is about an Offered
// translation, and a free viewer's is locked (Upsell), never Offered. Their
// default is still the phase-1 selection -- Accept-Language, then English --
// exactly as rule 5 of docs/subtitle_translate.md describes. Pinned so that
// widening the rule to them is a decision, not a side effect.
func TestFreeViewerKeepsThePhaseOneFallback(t *testing.T) {
	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]`)
	ud := enViewer(language.Portuguese, language.Russian, language.English)
	items := NewHelper().GetSubtitles(ud, audio, &ra.ExportTag{}, []api.OpenSubtitleTrack{osAbs("1", "ru", "hash"), osAbs("2", "en", "imdb")}, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	tr := byID(items)["tr-pt"]
	if !tr.Locked || !tr.Upsell || tr.Offered {
		t.Fatalf("fixture must produce a locked upsell: %+v", tr)
	}
	if d := defaultID(items); d != "os-1" {
		t.Fatalf("default=%s want os-1: the phase-1 Accept-Language pick", d)
	}
}

// TestSavedOffWithOfferSuggestsTheEnglishTrack: the viewer switched
// subtitles off earlier, and the ladder's answer is an offer. The switch
// restores what the ladder would have played -- the English track -- not
// offSuggestion's forced Portuguese track, which covers signs and nothing
// of the Japanese dialogue.
//
// The first half is the same file with nothing saved: that is the track the
// ladder plays, so the two halves agree.
//
// Negative control: without markSuggested(offeredDefault) in the saved-off
// branch, offSuggestion answers et-1 (its rung 2).
func TestSavedOffWithOfferSuggestsTheEnglishTrack(t *testing.T) {
	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}}]`)
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "https://x/forced-pt.vtt", SrcLang: "pt", Label: "Movie.pt.forced.srt", Kind: "subtitles"},
		{Src: "https://x/full-en.vtt", SrcLang: "en", Label: "Movie.en.srt", Kind: "subtitles"},
	}}

	on := NewHelper().GetSubtitles(enViewer(language.Portuguese), audio, tag, nil, &models.ExternalData{}, nil, paidPT)
	if !byID(on)["et-1"].Forced || !byID(on)["tr-pt"].Offered {
		t.Fatalf("fixture must hold a forced pt track and an offered translation: %+v", on)
	}
	if d := defaultID(on); d != "et-2" {
		t.Fatalf("default=%s want et-2 (the English track)", d)
	}

	ud := enViewer(language.Portuguese)
	ud.SubtitleID = "none"
	off := NewHelper().GetSubtitles(ud, audio, tag, nil, &models.ExternalData{}, nil, paidPT)
	if d := defaultID(off); d != "none" || !byID(off)["none"].Saved {
		t.Fatalf("default=%s: the saved off must stand", d)
	}
	if !byID(off)["tr-pt"].Offered {
		t.Fatal("the translation is still offered with subtitles off")
	}
	if s := suggestedID(t, off); s != "et-2" {
		t.Fatalf("suggested=%s want et-2: what the ladder plays when nothing is saved", s)
	}
}

// TestOfferedDefaultNeedsAFallbackLanguage: a VideoStreamUserData built
// without NewVideoStreamUserData has a zero FallbackLangTag. Its Base is a
// Low-confidence guess of English, and the phase-1 matcher reads the same
// tag as no language at all -- so the offer's default does too, and plays
// nothing. (The picker fixture of services/template relies on this.)
//
// Negative control: dropping fallbackLang's Exact check turns mp-0 on.
func TestOfferedDefaultNeedsAFallbackLanguage(t *testing.T) {
	tag, oss := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, oss, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["tr-pt"].Offered {
		t.Fatalf("fixture must offer a translation: %+v", byID(items)["tr-pt"])
	}
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none: no fallback language, no fallback track", d)
	}
}

// TestFallbackLangIsMirroredInSubtitleRules: the client re-runs the rule on
// an audio switch and needs the same language. It is a constant there
// (subtitle-rules.js, FALLBACK_LANG), so this keeps it equal to what the
// server uses for every real stream.
//
// Negative control: any other value in the JS constant reddens this.
func TestFallbackLangIsMirroredInSubtitleRules(t *testing.T) {
	src, err := os.ReadFile("../../assets/src/js/lib/player/subtitle-rules.js")
	if err != nil {
		t.Fatalf("subtitle-rules.js: %v", err)
	}
	m := regexp.MustCompile(`export const FALLBACK_LANG = '([a-z]+)';`).FindSubmatch(src)
	if m == nil {
		t.Fatal("FALLBACK_LANG is gone from subtitle-rules.js -- if the client stopped needing it, drop this test")
	}
	want := fallbackLang(models.NewVideoStreamUserData("r", "i", &models.StreamSettings{}))
	if want == "" || string(m[1]) != want {
		t.Fatalf("subtitle-rules.js FALLBACK_LANG=%q, the server falls back to %q", m[1], want)
	}
}

// TestNextEpisodeKeepsTheOffer: the player carries what is PLAYING to the
// next file (readCarry in next-item.js reads the chips, not a saved value),
// and the carried choice arrives as a saved one. Before 2026-09-28 an offer
// played "None", so the carry was "off" and the next episode offered the
// translation again (the saved-off branch). With the English track playing
// beside the offer, the carry is "English" -- and a saved track used to mean
// "the viewer has dealt with subtitles", withdrawing the offer from every
// episode after the first.
//
// A saved choice that is the very track the ladder plays beside the offer
// says nothing of the kind: the offer stands. Another English track -- one
// the viewer picked over the ladder's -- is a choice, and the offer goes, as
// it does for any other saved track.
//
// Negative control: without the saved-track branch in applyLadder the first
// half loses Offered.
func TestNextEpisodeKeepsTheOffer(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}]`)
	oss := []api.OpenSubtitleTrack{osAbs("1", "en", "hash")}

	carried := func(provider string) []ListItem {
		ud := enViewer(language.Portuguese)
		ud.Carry = &models.TrackCarry{Subtitles: "on", SubtitleLang: "en", SubtitleProvider: provider}
		return NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, oss, &models.ExternalData{}, nil, paidPT)
	}

	// The ladder's own English track (embedded, rank 1) carried over.
	items := carried("MediaProbe")
	if d := defaultID(items); d != "mp-0" || !byID(items)["mp-0"].Saved {
		t.Fatalf("default=%s: the carried track plays, as a saved choice", d)
	}
	if tr := byID(items)["tr-pt"]; !tr.Offered || tr.Default {
		t.Fatalf("the translation is still on offer on the next episode: %+v", tr)
	}

	// Another English track than the ladder's: the viewer chose it.
	items = carried("OpenSubtitles")
	if d := defaultID(items); d != "os-1" {
		t.Fatalf("default=%s want os-1", d)
	}
	if byID(items)["tr-pt"].Offered {
		t.Fatal("a track the viewer picked over the ladder's withdraws the offer, like any saved track")
	}
}

// TestOfferEnglishIsADeclaredLanguage: beside an offer only a track that
// DECLARES English plays. ffprobe reports no language tag for a Matroska
// track marked "und", and GetSubtitles labels such a stream English for
// display -- in the 2026-09-14 probe sample 7.8% of files with text
// subtitles had one, titled "Español", "Chinese", "rus full". Embedded is
// rank 1, so without this it beat the real English OpenSubtitles track and
// a Portuguese viewer got Spanish subtitles as "English". An "und" tag
// itself is the same case: its Base is CLDR's Low-confidence guess of
// English.
//
// Negative controls: without the LangGuessed test in offeredDefault the
// first half answers mp-0; with baseLang instead of declaredLang the
// second answers et-1.
func TestOfferEnglishIsADeclaredLanguage(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"title":"Español"}}]`)
	items := NewHelper().GetSubtitles(enViewer(language.Portuguese), mp, &ra.ExportTag{}, []api.OpenSubtitleTrack{osAbs("1", "en", "hash")}, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["tr-pt"].Offered || !byID(items)["mp-0"].LangGuessed {
		t.Fatalf("fixture must offer a translation beside an untagged embedded track: %+v", items)
	}
	if d := defaultID(items); d != "os-1" {
		t.Fatalf("default=%s want os-1: the untagged stream is not known to be English", d)
	}

	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]`)
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/a.vtt", SrcLang: "und", Label: "Movie.srt", Kind: "subtitles"}}}
	items = NewHelper().GetSubtitles(enViewer(language.Portuguese), audio, tag, []api.OpenSubtitleTrack{osAbs("2", "fr", "hash")}, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["tr-pt"].Offered {
		t.Fatalf("fixture must offer a translation: %+v", items)
	}
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none: an \"und\" track is not English", d)
	}
}

// TestOfferEnglishIsAFullTrack: a forced English track is signs only, and
// beside an offer the viewer needs the dialogue -- the full track plays
// although the forced sidecar ranks better (2 against OpenSubtitles' 3).
// The client pins the same (subtitle-rules.test.js, "A forced English
// track is signs only").
//
// Negative control: letting forced tracks into offeredDefault answers et-1.
func TestOfferEnglishIsAFullTrack(t *testing.T) {
	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}}]`)
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/f.vtt", SrcLang: "en", Label: "Movie.en.forced.srt", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(enViewer(language.Portuguese), audio, tag, []api.OpenSubtitleTrack{osAbs("1", "en", "hash")}, &models.ExternalData{}, nil, paidPT)
	if !byID(items)["et-1"].Forced || !byID(items)["tr-pt"].Offered {
		t.Fatalf("fixture must hold a forced English sidecar beside an offer: %+v", items)
	}
	if d := defaultID(items); d != "os-1" {
		t.Fatalf("default=%s want os-1: the full English track, not the signs", d)
	}
}

// TestCarriedEnglishNeverOffersAHumanTrack: the saved-track exception in
// applyLadder re-marks the ladder's pick Offered only when that pick is a
// translation. Episode 2 of a series can have what episode 1 lacked -- a
// Portuguese track -- and then the ladder's pick is that human track: an
// Offered mark on it would draw a real subtitle as "Translate to
// Portuguese". Which track PLAYS here is not pinned: the carried English
// wins over the Portuguese one, a known gap of carrying the ladder's pick
// as a choice (review 2026-09-28).
//
// Negative control: dropping `lis[p].Provider == "Translated"` from that
// branch marks os-2 Offered.
func TestCarriedEnglishNeverOffersAHumanTrack(t *testing.T) {
	audio := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]`)
	ud := enViewer(language.Portuguese)
	ud.Carry = &models.TrackCarry{Subtitles: "on", SubtitleLang: "en", SubtitleProvider: "OpenSubtitles"}
	items := NewHelper().GetSubtitles(ud, audio, &ra.ExportTag{}, []api.OpenSubtitleTrack{osAbs("1", "en", "hash"), osAbs("2", "pt", "hash")}, &models.ExternalData{}, nil, paidPT)
	for _, li := range items {
		if li.Offered && li.Provider != "Translated" {
			t.Fatalf("%s is a human track and was marked Offered: %+v", li.ID, li)
		}
	}
}
