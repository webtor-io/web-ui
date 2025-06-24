package stremio

type StreamItem struct {
	Title       string `json:"title"`
	InfoHash    string `json:"infoHash,omitempty"`
	FileIdx     uint8  `json:"fileIdx,omitempty"`
	Url         string `json:"url,omitempty"`
	YtId        string `json:"ytId,omitempty"`
	ExternalUrl string `json:"externalUrl,omitempty"`
}

type StreamsResponse struct {
	Streams []StreamItem `json:"streams"`
}

type MetaItem struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Genres      []string    `json:"genres,omitempty"`
	Poster      string      `json:"poster"`
	ReleaseInfo string      `json:"releaseInfo,omitempty"`
	PosterShape string      `json:"posterShape,omitempty"`
	Videos      []VideoItem `json:"videos,omitempty"`
}

type VideoItem struct {
	Name    string `json:"name"`
	Episode int    `json:"episode"`
	Season  int    `json:"season"`
	ID      string `json:"id"`
}

type MetasResponse struct {
	Metas []MetaItem `json:"metas"`
}

type MetaResponse struct {
	Meta MetaItem `json:"meta"`
}

type CatalogItem struct {
	Type string `json:"type"`
	Id   string `json:"id"`
}

type Manifest struct {
	Id            string         `json:"id"`
	Version       string         `json:"version"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Types         []string       `json:"types"`
	Catalogs      []CatalogItem  `json:"catalogs"`
	Resources     []string       `json:"resources"`
	Logo          string         `json:"logo,omitempty"`
	Background    string         `json:"background,omitempty"`
	ContactEmail  string         `json:"contactEmail,omitempty"`
	BehaviorHints *BehaviorHints `json:"behaviorHints,omitempty"`
	Config        []ConfigOption `json:"config,omitempty"`
}

type BehaviorHints struct {
	Configurable          bool `json:"configurable,omitempty"`
	ConfigurationRequired bool `json:"configurationRequired,omitempty"`
	Adult                 bool `json:"adult,omitempty"`
	NotWebReady           bool `json:"notWebReady,omitempty"`
	DeepLinking           bool `json:"deepLinking,omitempty"`
}

type ConfigOption struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Label    string   `json:"label,omitempty"`
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
}
