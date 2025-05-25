package models

type EmbedSettings struct {
	StreamSettings
	Version    string `json:"version"`
	Magnet     string `json:"magnet"`
	TorrentURL string `json:"torrentUrl"`
	Referer    string `json:"referer"`
	PWD        string `json:"pwd"`
	File       string `json:"file"`
	Path       string `json:"path"`
}
