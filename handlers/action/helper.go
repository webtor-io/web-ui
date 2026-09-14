package action

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"regexp"
	"strconv"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/stremio"
	"golang.org/x/text/language"
)

type ListItem struct {
	ID       string
	MPID     string
	Label    string
	Default  bool
	SrcLang  string
	Provider string
	Src      string
	Kind     string
	Source   string
	// Preload marks side-loaded tracks rendered as <track> elements in the
	// page. Browsers fetch every <track> on load regardless of mode, so
	// only the default track plus the viewer's language and English are
	// rendered (capped by maxPreloadTracks); the rest are created on
	// selection by the player. Embedded tracks are never <track> elements.
	Preload bool
	// Forced marks "signs only" tracks (foreign-language lines and
	// on-screen text). They are never a full subtitle by the ladder and
	// never a translation source; they are the default when the audio is
	// already in the viewer's language.
	Forced bool
	// Locked is a track the viewer may not activate (AI translation for a
	// free account): rendered with a lock and a CTA, no Src.
	Locked bool
	// Badge names the origin for the picker: user, embedded, sidecar, os,
	// ai, forced (i18n key action.stream.badge.<Badge>).
	Badge string
	// Rank is the item's place in the ladder (see ladderRank): the player
	// renders it as data-rank and reuses it when it has to pick a default
	// itself, so the order lives in Go only and is never reimplemented in
	// JS.
	Rank int
	// SourceBadge is set on the Translated item alone: the Badge of the
	// human track the translation is made from, so the picker can say
	// "AI - from <origin>" without knowing the ladder.
	SourceBadge string
	// SourceID is set on the Translated item alone: the ID of the list
	// item being translated. Diagnostics only -- it is not rendered and
	// not reported, unlike Source, which stays the OpenSubtitles
	// hash|imdb enum for every provider.
	SourceID string
}

// SubtitleOpts is defined once in models (see models/subtitle_opts.go);
// this alias keeps the action package's call sites and signatures short.
type SubtitleOpts = models.SubtitleOpts

// maxPreloadTracks bounds how many side-loaded tracks are rendered as
// <track> elements: enough for the native iOS subtitle menu to offer the
// viewer's language and English, few enough that the page-load burst
// stays well under the proxy's per-session concurrency caps.
const maxPreloadTracks = 8

type Helper struct {
}

func NewHelper() *Helper {
	return &Helper{}
}

// UserSubtitleView builds the flat data struct the user_subtitles_view
// partial expects. Called from the stream_video template so that the
// initial render and the async-reload response (from the /user-subtitle
// handler) feed the same shape into the same partial.
func (s *Helper) UserSubtitleView(resourceID, path, eiURL string, subs []models.UserSubtitleTrack) *models.UserSubtitleView {
	return &models.UserSubtitleView{
		ResourceID:    resourceID,
		Path:          path,
		EIURL:         eiURL,
		UserSubtitles: subs,
	}
}

func (s *Helper) GetDurationSec(mp *api.MediaProbe) string {
	return mp.Format.Duration
}

func (s *Helper) HasControls(settings *models.StreamSettings) bool {
	if settings.Controls == nil {
		return true
	}
	controls := *settings.Controls
	return controls
}

func (s *Helper) GetAudioTracks(ud *models.VideoStreamUserData, mp *api.MediaProbe) []ListItem {
	var res []ListItem
	if mp == nil {
		res = append(res, ListItem{
			ID:    "1",
			Label: "Audio #1",
		})
	} else {
		i := 0
		for _, stream := range mp.Streams {
			if stream.CodecType == "audio" {
				meta := ""
				if stream.ChannelLayout != "" {
					meta = stream.ChannelLayout
				}
				if meta != "" {
					meta = " (" + meta + ")"
				}
				title := fmt.Sprintf("Audio%v #%v", meta, i+1)
				if stream.Tags.Title != "" {
					title = stream.Tags.Title + meta
				}

				res = append(res, ListItem{
					ID:       "mp-" + strconv.Itoa(i),
					MPID:     strconv.Itoa(i),
					Label:    title,
					SrcLang:  stream.Tags.Language,
					Provider: "MediaProbe",
				})
				i++
			}
		}
	}
	return s.selectListItem(s.canonizeSrcLangs(res), ud.AudioID, ud)
}

type langIndex map[language.Tag]int

func (s *Helper) selectListItem(lis []ListItem, id string, ud *models.VideoStreamUserData) []ListItem {
	if len(lis) == 0 {
		return lis
	}
	for i, li := range lis {
		if li.ID == id {
			lis[i].Default = true
			return lis
		}
	}
	for _, li := range lis {
		if li.Default {
			return lis
		}
	}

	index, err := s.matchLang(lis, ud)
	if err != nil {
		lis[0].Default = true
		return lis
	}
	lis[index].Default = true
	return lis
}

func (s *Helper) matchLang(lis []ListItem, ud *models.VideoStreamUserData) (lIndex int, err error) {
	lx := langIndex{}
	for i, li := range lis {
		// Forced (signs-only) tracks are never a candidate for automatic
		// language-based selection: they're not a full subtitle track, and
		// picking one silently instead of "no subtitle" or a real track in
		// the viewer's language would be a worse default. Task 3 adds the
		// one case where a forced track IS the right default (forced track
		// in the preferred language while the audio is already in that
		// language) as a rule on top of this, not by loosening this one.
		if li.Forced {
			continue
		}
		// AI translations are never a candidate here either. applyLadder
		// promotes the AI item itself when the viewer may activate it;
		// everything that reaches this function is a fallback, and a
		// fallback that lands on a locked item selects a track with no
		// Src -- subtitles "on" and nothing on screen.
		if li.Provider == "Translated" {
			continue
		}
		if t, err := language.Parse(li.SrcLang); err == nil {
			if _, ok := lx[t]; !ok {
				lx[t] = i
			}
		}
	}
	var langs []language.Tag
	for t := range lx {
		langs = append(langs, t)
	}
	matcher := language.NewMatcher(langs)
	_, index, confidence := matcher.Match(ud.AcceptLangTags...)
	if confidence > language.No {
		lIndex = lx[langs[index]]
		return
	}
	_, index, confidence = matcher.Match(ud.FallbackLangTag)
	if confidence > language.No {
		lIndex = lx[langs[index]]
		return
	}
	err = errors.New("no accept lang")
	return
}

func (s *Helper) canonizeSrcLangs(lis []ListItem) []ListItem {
	for i, li := range lis {
		if t, err := language.Parse(li.SrcLang); err == nil {
			lis[i].SrcLang = t.String()
			//lis[i].Label = display.English.Tags().FieldType(t)
		}
	}
	return lis
}

func (s *Helper) FilterSubtitlesByProvider(subs []ListItem, provider string, exclude bool) []ListItem {
	var res []ListItem
	for _, s := range subs {
		if s.Provider == provider && !exclude {
			res = append(res, s)
		} else if s.Provider != provider && exclude {
			res = append(res, s)
		}
	}
	return res
}

// Bitmap subtitle codecs cannot be rendered by the browser (they need
// OCR) and cannot be translated. hdmv_pgs is also dropped by
// content-transcoder from the HLS subtitle group, so it must not
// consume an MPID; the other bitmap codecs stay in the group and keep
// their slot even though we hide them.
var bitmapSubtitleCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"dvd_subtitle":      true,
	"dvb_subtitle":      true,
	"xsub":              true,
}

// forcedTitleRe matches "forced" as a whole word. A plain substring
// test also hides "Unforced" and "Reinforced", losing a real subtitle
// stream from the picker.
var forcedTitleRe = regexp.MustCompile(`(?i)\bforced\b`)

// embeddedSubtitleVisible reports whether an embedded subtitle stream
// is offered in the picker, whether it occupies an index in the
// transcoder's HLS subtitle group (see content-transcoder
// services/hls.go: everything but hdmv_pgs is included), and whether
// it is a "forced" (signs-only) track by title until content-prober
// exposes ffprobe's disposition flags.
func embeddedSubtitleVisible(codecName, title string) (visible bool, countsForHLS bool, forced bool) {
	if codecName == "hdmv_pgs_subtitle" {
		return false, false, false
	}
	if bitmapSubtitleCodecs[codecName] {
		return false, true, false
	}
	return true, true, forcedTitleRe.MatchString(title)
}

// sidecarForced reports whether a side-loaded (ExportTag) track is a
// "forced" (signs-only) track by its label or source URL.
func sidecarForced(label, src string) bool {
	return forcedTitleRe.MatchString(label) || forcedTitleRe.MatchString(src)
}

// badgeFor names the origin badge shown on a list item (i18n key
// action.stream.badge.<Badge>). A forced track always gets "forced"
// regardless of provider, since that is the more useful signal to the
// viewer than where the track came from.
func badgeFor(provider string, forced bool) string {
	if forced {
		return "forced"
	}
	switch provider {
	case "UserSubtitle":
		return "user"
	case "MediaProbe":
		return "embedded"
	case "ExportTag", "External":
		return "sidecar"
	case "OpenSubtitles":
		return "os"
	case "Translated":
		return "ai"
	}
	return ""
}

// baseLang reduces a language tag to its base ("por" and "pt-BR" both
// become "pt"), the granularity every rule of the ladder works at. An
// unparseable or undetermined tag yields "", which the rules read as
// "language unknown".
func baseLang(tag string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return ""
	}
	b, conf := t.Base()
	if conf == language.No {
		return ""
	}
	return b.String()
}

// defaultAudioLang is the base language of the audio the viewer will
// actually hear: the Default item of GetAudioTracks (the Accept-Language
// match), not the first audio stream of the probe. "" when the language
// of that track is unknown.
func (s *Helper) defaultAudioLang(ud *models.VideoStreamUserData, mp *api.MediaProbe) string {
	for _, a := range s.GetAudioTracks(ud, mp) {
		if a.Default {
			return baseLang(a.SrcLang)
		}
	}
	return ""
}

// rankUnknown is the rank of an item outside the ladder ("None", and any
// provider added later without a rank of its own): it always sorts last.
// Ranks 6-8 are reserved for sources between the AI translation and
// unknown (phase 3 whisper transcription takes 6).
const rankUnknown = 9

// ladderRank orders subtitle sources from the one the viewer trusts most
// (what they uploaded themselves) to the one they trust least (a machine
// translation). Within OpenSubtitles a hash match is a match on this very
// file, while an imdb match is only the same title, so it can be out of
// sync. The rank is also rendered as data-rank: the player reuses this
// order when it has to pick a default itself instead of reimplementing
// the ladder in JS.
func ladderRank(li ListItem) int {
	switch li.Provider {
	case "UserSubtitle":
		return 0
	case "MediaProbe":
		return 1
	case "ExportTag", "External":
		return 2
	case "OpenSubtitles":
		if li.Source == "hash" {
			return 3
		}
		return 4
	case "Translated":
		return 5
	}
	return rankUnknown
}

// isHumanFull reports whether the item is a complete, human-made subtitle
// track: not the "None" entry, not a signs-only (forced) track, not a
// machine translation.
func isHumanFull(li ListItem) bool {
	return li.ID != "none" && !li.Forced && li.Provider != "Translated"
}

// bestByLadder returns the index of the best item in lang by ladderRank,
// among forced or among full tracks (never mixing the two), or -1.
func bestByLadder(lis []ListItem, lang string, forced bool) int {
	best, rank := -1, 99
	for i, li := range lis {
		if li.ID == "none" || li.Provider == "Translated" || li.Forced != forced || baseLang(li.SrcLang) != lang {
			continue
		}
		if r := ladderRank(li); r < rank {
			best, rank = i, r
		}
	}
	return best
}

// pickTranslationSource picks the human track the AI translation is made
// from: a non-forced, URL-backed track, preferring the audio language (a
// transcription of what is being said, not a translation of a
// translation), then English, then anything. Embedded tracks have no URL
// of their own to feed the proxy chain, so they cannot be a source yet.
func pickTranslationSource(lis []ListItem, audioLang string) *ListItem {
	var first, en, audio *ListItem
	for i := range lis {
		li := &lis[i]
		if !isHumanFull(*li) || li.Src == "" || li.Provider == "MediaProbe" {
			continue
		}
		if first == nil {
			first = li
		}
		switch baseLang(li.SrcLang) {
		case audioLang:
			if audio == nil && audioLang != "" {
				audio = li
			}
		case "en":
			if en == nil {
				en = li
			}
		}
	}
	if audio != nil {
		return audio
	}
	if en != nil {
		return en
	}
	return first
}

// applyLadder decides what the viewer gets selected when they have a
// preferred content language: the best human track in that language, or
// an AI translation when there is none, or nothing at all when the audio
// is already in that language (then only a forced track, if the file has
// one, is turned on). The viewer's own saved choice always wins. When the
// preferred language yields nothing at all, the phase-1 selection
// (Accept-Language, then the English fallback) still applies: a language
// the ladder cannot serve must not switch subtitles off.
//
// lis always starts with the "None" item, which GetSubtitles prepends
// before anything else, so lis[0] is the safe "no subtitles" default.
func (s *Helper) applyLadder(lis []ListItem, ud *models.VideoStreamUserData, audioLang string, opts SubtitleOpts) []ListItem {
	lang := opts.PreferredLang
	humanIdx := bestByLadder(lis, lang, false)
	if humanIdx < 0 && opts.Translate {
		// A language the translation service does not know is not offered
		// at all: an item leading to a rejected request is worse than no
		// item.
		if l := stremio.LanguageByCode(lang); l != nil {
			if src := pickTranslationSource(lis, audioLang); src != nil {
				tr := ListItem{
					ID:       "tr-" + lang,
					Label:    l.Name + " · AI",
					SrcLang:  lang,
					Kind:     "subtitles",
					Provider: "Translated",
					Badge:    badgeFor("Translated", false),
					// SourceBadge is the origin shown in the picker,
					// SourceID the item it came from; Source stays empty
					// (it is the OpenSubtitles hash|imdb enum).
					SourceBadge: src.Badge,
					SourceID:    src.ID,
				}
				tr.Rank = ladderRank(tr)
				if opts.Paid {
					tr.Src = api.TranslateURL(src.Src, lang, opts.Names)
				} else {
					// A locked item carries no Src on purpose: the URL is
					// the entitlement, so a free viewer must not receive
					// one even hidden in the markup.
					tr.Locked = true
				}
				lis = append(lis, tr)
			}
		}
	}
	// The viewer's saved choice always wins, and is the only default:
	// ExternalData may have marked a track Default already. A choice that
	// now points at a locked item (an AI track saved while the viewer was
	// paying) is ignored, and the ladder decides as if nothing was saved.
	if ud.SubtitleID != "" {
		for i := range lis {
			if lis[i].ID == ud.SubtitleID && !lis[i].Locked {
				for j := range lis {
					lis[j].Default = false
				}
				lis[i].Default = true
				return lis
			}
		}
	}
	// An embed that asked for a specific track (ExternalData) has already
	// marked it Default; that is the caller's explicit choice, and the
	// ladder neither overrides it nor adds a second default to the list.
	for _, li := range lis {
		if li.Default {
			return lis
		}
	}
	// Subtitles are not needed when the audio is already in the preferred
	// language. lang is never "" here (GetSubtitles takes the legacy path
	// then), so an unknown audio language ("") counts as needed.
	if audioLang == lang {
		if f := bestByLadder(lis, lang, true); f >= 0 {
			lis[f].Default = true
		} else {
			lis[0].Default = true // "None"
		}
		return lis
	}
	if humanIdx >= 0 {
		lis[humanIdx].Default = true
		return lis
	}
	for i := range lis {
		// A locked item cannot be turned on, so it cannot be the default
		// either: the free viewer would face a player with subtitles
		// "selected" and nothing on screen.
		if lis[i].Provider == "Translated" && !lis[i].Locked {
			lis[i].Default = true
			return lis
		}
	}
	// The preferred language yielded nothing activatable. Falling through
	// to "None" would take subtitles away from viewers who had them in
	// phase 1, so the old Accept-Language selection decides instead.
	return s.selectListItem(lis, "", ud)
}

func (s *Helper) GetSubtitles(ud *models.VideoStreamUserData, mp *api.MediaProbe, tag *ra.ExportTag, opensubs []api.OpenSubtitleTrack, ext *models.ExternalData, userSubs []models.UserSubtitleTrack, opts SubtitleOpts) []ListItem {
	var res []ListItem
	res = append(res, ListItem{
		ID:    "none",
		Label: "None",
		Kind:  "subtitles",
	})
	if mp != nil {
		i := 0
		for _, stream := range mp.Streams {
			if stream.CodecType != "subtitle" {
				continue
			}
			visible, counts, forced := embeddedSubtitleVisible(stream.CodecName, stream.Tags.Title)
			if !counts {
				continue
			}
			if visible {
				label := fmt.Sprintf("Subtitle #%v", i+1)
				if stream.Tags.Title != "" {
					label = stream.Tags.Title
				}
				srcLang := "eng"
				if stream.Tags.Language != "" {
					srcLang = stream.Tags.Language
				}
				res = append(res, ListItem{
					ID:       "mp-" + strconv.Itoa(i),
					MPID:     strconv.Itoa(i),
					Label:    label,
					SrcLang:  srcLang,
					Kind:     "subtitles",
					Provider: "MediaProbe",
					Forced:   forced,
					Badge:    badgeFor("MediaProbe", forced),
				})
			}
			i++
		}
	}
	for i, t := range tag.Tracks {
		forced := sidecarForced(t.Label, t.Src)
		res = append(res, ListItem{
			ID:       "et-" + strconv.Itoa(i+1),
			Label:    t.Label,
			SrcLang:  t.SrcLang,
			Kind:     string(t.Kind),
			Src:      t.Src,
			Provider: "ExportTag",
			Forced:   forced,
			Badge:    badgeFor("ExportTag", forced),
		})
	}
	for _, t := range opensubs {
		res = append(res, ListItem{
			ID:       "os-" + t.ID,
			Label:    t.Label,
			SrcLang:  t.SrcLang,
			Kind:     string(t.Kind),
			Src:      t.Src,
			Provider: "OpenSubtitles",
			Source:   t.Source,
			Badge:    badgeFor("OpenSubtitles", false),
		})
	}
	for i, t := range ext.Tracks {
		res = append(res, ListItem{
			ID:       "ext-" + strconv.Itoa(i+1),
			Label:    t.Label,
			SrcLang:  t.SrcLang,
			Default:  t.Default,
			Kind:     "subtitles",
			Src:      t.Src,
			Provider: "External",
			Badge:    badgeFor("External", false),
		})
	}
	for _, t := range userSubs {
		res = append(res, ListItem{
			ID:       t.ID,
			Label:    t.Label,
			SrcLang:  t.SrcLang,
			Kind:     "subtitles",
			Src:      t.Src,
			Provider: "UserSubtitle",
			Badge:    badgeFor("UserSubtitle", false),
		})
	}
	lis := s.canonizeSrcLangs(res)
	for i := range lis {
		lis[i].Rank = ladderRank(lis[i])
	}
	if opts.PreferredLang == "" {
		return s.markPreload(s.selectListItem(lis, ud.SubtitleID, ud), ud)
	}
	return s.markPreload(s.applyLadder(lis, ud, s.defaultAudioLang(ud, mp), opts), ud)
}

// markPreload sets Preload on the default track and on side-loaded tracks in
// the viewer's preferred language or English, in list order, up to
// maxPreloadTracks.
func (s *Helper) markPreload(lis []ListItem, ud *models.VideoStreamUserData) []ListItem {
	wanted := map[string]bool{"en": true}
	if ud != nil && len(ud.AcceptLangTags) > 0 {
		if base, conf := ud.AcceptLangTags[0].Base(); conf != language.No {
			wanted[base.String()] = true
		}
	}
	n := 0
	for i, li := range lis {
		// The Translated item is never a <track> in the page: the
		// translation is produced on demand and polled by the player, so
		// preloading it would start that work for every viewer.
		if li.ID == "none" || li.Provider == "MediaProbe" || li.Src == "" || li.Provider == "Translated" {
			continue
		}
		if n >= maxPreloadTracks {
			break
		}
		if li.Default {
			lis[i].Preload = true
			n++
			continue
		}
		if t, err := language.Parse(li.SrcLang); err == nil {
			if base, _ := t.Base(); wanted[base.String()] {
				lis[i].Preload = true
				n++
			}
		}
	}
	return lis
}
