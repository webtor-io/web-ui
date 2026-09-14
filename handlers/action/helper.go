package action

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"regexp"
	"strconv"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
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
	return s.markPreload(s.selectListItem(s.canonizeSrcLangs(res), ud.SubtitleID, ud), ud)
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
		if li.ID == "none" || li.Provider == "MediaProbe" || li.Src == "" {
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
