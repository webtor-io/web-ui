package action

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
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
}

type Helper struct {
}

func NewHelper() *Helper {
	return &Helper{}
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

func (s *Helper) GetSubtitles(ud *models.VideoStreamUserData, mp *api.MediaProbe, tag *ra.ExportTag, opensubs []api.OpenSubtitleTrack, ext *models.ExternalData) []ListItem {
	var res []ListItem
	res = append(res, ListItem{
		ID:    "none",
		Label: "None",
		Kind:  "subtitles",
	})
	if mp != nil {
		i := 0
		for _, stream := range mp.Streams {
			if stream.CodecType == "subtitle" {
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
				i++
			}
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
	return s.selectListItem(s.canonizeSrcLangs(res), ud.SubtitleID, ud)
}
