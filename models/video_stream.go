package models

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
)

type VideoStreamUserData struct {
	ResourceID      string
	ItemID          string
	SubtitleID      string
	AudioID         string
	AcceptLangTags  []language.Tag
	FallbackLangTag language.Tag
	Settings        *StreamSettings
}

func NewVideoStreamUserData(resourceID string, itemID string, settings *StreamSettings) *VideoStreamUserData {
	return &VideoStreamUserData{
		ResourceID:      resourceID,
		ItemID:          itemID,
		FallbackLangTag: language.English,
		Settings:        settings,
	}
}

func (s *VideoStreamUserData) FetchSessionData(c *gin.Context) {
	session := sessions.Default(c)
	var subtitleID, audioID string
	audioKey := s.makeKey(s.ResourceID, s.ItemID, "audio")
	subtitleKey := s.makeKey(s.ResourceID, s.ItemID, "subtitle")
	if session.Get(subtitleKey) != nil {
		subtitleID = session.Get(subtitleKey).(string)
	}
	if session.Get(audioKey) != nil {
		audioID = session.Get(audioKey).(string)
	}
	accept := c.GetHeader("Accept-Language")
	if s.Settings.UserLang != "" {
		accept = s.Settings.UserLang
	}
	tags, _, err := language.ParseAcceptLanguage(accept)
	if err != nil {
		tags = []language.Tag{language.English}
	}
	s.AudioID = audioID
	s.SubtitleID = subtitleID
	s.AcceptLangTags = tags
}

func (s *VideoStreamUserData) makeKey(resourceID string, itemID string, name string) string {
	return fmt.Sprintf("%v_%v_%v_id", resourceID, itemID, name)
}

func (s *VideoStreamUserData) UpdateSessionData(c *gin.Context) error {
	session := sessions.Default(c)
	audioKey := s.makeKey(s.ResourceID, s.ItemID, "audio")
	subtitleKey := s.makeKey(s.ResourceID, s.ItemID, "subtitle")
	if s.SubtitleID == "" {
		session.Delete(subtitleKey)
	} else {
		session.Set(subtitleKey, s.SubtitleID)
	}
	if s.AudioID == "" {
		session.Delete(audioKey)
	} else {
		session.Set(audioKey, s.AudioID)
	}
	return session.Save()
}

type ExternalData struct {
	Poster string
	Tracks []ExternalTrack
}

type ExternalTrack struct {
	Src     string
	SrcLang string
	Label   string
	Default bool
}

// UserSubtitleTrack is a per-user uploaded subtitle prepared for rendering:
// Src is wrapped through torrent-http-proxy's /ext/ (plus ~vtt/ for SRT),
// so the same value drives the static <track> at initial render and the
// dynamically-injected <track> on first click after async upload. DeleteURL
// is the canonical path the "My Subtitles" tab POSTs to.
type UserSubtitleTrack struct {
	ID           string
	Src          string
	Label        string
	Format       string
	Size         int64
	DeleteURL    string
	OriginalName string
	// SrcLang is the BCP-47 tag rendered into <track srclang>. HTML requires
	// the attribute for kind="subtitles"; "und" stands in when the filename
	// declares no language.
	SrcLang string
	// Selected marks the track the viewer should end up watching with. Set
	// only on the response to an upload: the player switches to it without a
	// second click. A plain list render leaves every track unselected so a
	// re-render never overrides a choice already made.
	Selected bool
	// Default and Saved mirror the same fields of the corresponding
	// action.ListItem (matched by ID) so the "My Subtitles" tab marks its
	// rows exactly like the other two lists. They are separate from
	// Selected, which is about the upload that just happened: Default is
	// "this is the track playing", Saved is "the viewer chose it
	// themselves", and the player's audio-switch rule reads both off the
	// DOM. Without them an upload the viewer had chosen was invisible to
	// that rule and got re-decided over.
	Default bool
	Saved   bool
	// Suggested mirrors action.ListItem.Suggested: the track the picker's
	// subtitles switch would turn on while they are off. Uploads are rank 0
	// on the ladder, so this is the row it lands on most often — and
	// without copying it here the chip the client has to find by
	// data-suggested was the one chip that never carried it.
	Suggested bool
}

// UserSubtitleView is the flat data shape consumed by the
// "user_subtitles_view" partial. Both the initial render (inside the stream
// modal) and the async reload (after upload/delete) feed this struct to the
// partial so their outputs are identical.
type UserSubtitleView struct {
	ResourceID    string
	Path          string
	EIURL         string
	UserSubtitles []UserSubtitleTrack
	ErrKey        string
	// ExpandedLang is the language the picker's track row opens on, so the
	// uploads' chips are collapsed by the same rule as every other chip and
	// the no-JS page is consistent. Empty on the async reload, which has no
	// language row to consult: nothing is collapsed there and the client
	// re-applies the filter right after the swap.
	ExpandedLang string
	// SubtitlesOff is the state of the picker's switch at render time, taken
	// from the same ladder result Default/Saved/Suggested come from. The
	// partial needs it for one reason: while subtitles are off it is the
	// Suggested chip, not the Default one, that wears the check and the
	// fill, and this markup has to agree with the dialog's own track row
	// rather than wait for the client's first refresh. False on the async
	// reload, which has no ladder result to read.
	SubtitlesOff bool
}
