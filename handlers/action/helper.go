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
}

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
// is offered in the picker and whether it occupies an index in the
// transcoder's HLS subtitle group (see content-transcoder
// services/hls.go: everything but hdmv_pgs is included). "Forced"
// tracks carry only foreign-language lines and are hidden by title
// until content-prober exposes ffprobe's disposition flags.
func embeddedSubtitleVisible(codecName, title string) (visible bool, countsForHLS bool) {
	if codecName == "hdmv_pgs_subtitle" {
		return false, false
	}
	if bitmapSubtitleCodecs[codecName] {
		return false, true
	}
	if forcedTitleRe.MatchString(title) {
		return false, true
	}
	return true, true
}

func (s *Helper) GetSubtitles(ud *models.VideoStreamUserData, mp *api.MediaProbe, tag *ra.ExportTag, opensubs []api.OpenSubtitleTrack, ext *models.ExternalData, userSubs []models.UserSubtitleTrack) []ListItem {
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
			visible, counts := embeddedSubtitleVisible(stream.CodecName, stream.Tags.Title)
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
				})
			}
			i++
		}
	}
	for i, t := range tag.Tracks {
		res = append(res, ListItem{
			ID:       "et-" + strconv.Itoa(i+1),
			Label:    t.Label,
			SrcLang:  t.SrcLang,
			Kind:     string(t.Kind),
			Src:      t.Src,
			Provider: "ExportTag",
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
		})
	}
	return s.selectListItem(s.canonizeSrcLangs(res), ud.SubtitleID, ud)
}
