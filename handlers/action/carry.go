package action

import (
	"strings"

	"github.com/webtor-io/web-ui/models"
)

// A track choice carried over from the previous file (models.TrackCarry) is
// an intent -- "English audio", "Russian subtitles from OpenSubtitles", "off"
// -- because track ids mean nothing across files. These two resolve it
// against THIS file's lists to an item id, which then goes down the one path
// that already knows every rule about a viewer's choice (locked items, the
// "None" item and what its switch restores, Saved): the saved-choice path.
// "" means the intent finds nothing here, and the file's own saved choice or
// the ladder decides as usual.

func carryAudioID(lis []ListItem, c *models.TrackCarry) string {
	if c == nil || c.AudioLang == "" {
		return ""
	}
	first := ""
	for _, li := range lis {
		if !strings.EqualFold(li.SrcLang, c.AudioLang) {
			continue
		}
		// Two tracks in one language are usually the film and a commentary,
		// or stereo and 5.1: the label is what told them apart last time.
		if c.AudioLabel != "" && li.Label == c.AudioLabel {
			return li.ID
		}
		if first == "" {
			first = li.ID
		}
	}
	return first
}

func carrySubtitleID(lis []ListItem, c *models.TrackCarry) string {
	if c == nil {
		return ""
	}
	switch c.Subtitles {
	case "off":
		for _, li := range lis {
			if li.ID == "none" {
				return li.ID
			}
		}
		return ""
	case "on":
	default:
		return ""
	}
	if c.SubtitleLang == "" {
		return ""
	}
	anyOrigin := ""
	for _, li := range lis {
		if li.ID == "none" || li.Locked || !strings.EqualFold(li.SrcLang, c.SubtitleLang) {
			continue
		}
		if c.SubtitleProvider != "" && li.Provider == c.SubtitleProvider {
			return li.ID
		}
		if anyOrigin == "" {
			anyOrigin = li.ID
		}
	}
	// The same language from another origin beats falling back to the ladder:
	// the viewer asked for Russian, not for OpenSubtitles.
	return anyOrigin
}

// withCarriedSubtitle returns ud, or a copy of it whose SubtitleID is the
// carried choice resolved against lis. A copy: ud is shared with the audio
// list and the rest of the render.
func withCarriedSubtitle(ud *models.VideoStreamUserData, lis []ListItem) *models.VideoStreamUserData {
	if ud == nil {
		return ud
	}
	id := carrySubtitleID(lis, ud.Carry)
	if id == "" {
		return ud
	}
	cp := *ud
	cp.SubtitleID = id
	return &cp
}
