package models

type SettingsTrack struct {
	Src     string  `json:"src"`
	SrcLang string  `json:"srclang,omitempty"`
	Label   string  `json:"label,omitempty"`
	Default *string `json:"default,omitempty"`
}

type StreamSettings struct {
	BaseURL   string          `json:"baseUrl"`
	Width     string          `json:"width"`
	Height    string          `json:"height"`
	Mode      string          `json:"mode"`
	Subtitles []SettingsTrack `json:"subtitles"`
	Poster    string          `json:"poster"`
	Header    bool            `json:"header"`
	Title     string          `json:"title"`
	ImdbID    string          `json:"imdbId"`
	Lang      string          `json:"lang"`
	I18n      struct{}        `json:"i18n"`
	Features  map[string]bool `json:"features"`
	El        struct{}        `json:"el"`
	Controls  *bool           `json:"controls"`
	UserLang  string          `json:"userLang"`
}
