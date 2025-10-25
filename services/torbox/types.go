package torbox

// User represents a TorBox user
type User struct {
	ID              int    `json:"id"`
	Email           string `json:"email"`
	Plan            int    `json:"plan"`
	TotalDownloaded int64  `json:"total_downloaded"`
	Customer        string `json:"customer"`
	ExpiresAt       string `json:"expires_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// Torrent represents a torrent in TorBox
type Torrent struct {
	ID               int     `json:"id"`
	Hash             string  `json:"hash"`
	Name             string  `json:"name"`
	Magnet           string  `json:"magnet"`
	Size             int64   `json:"size"`
	Progress         float64 `json:"progress"`
	Status           string  `json:"status"`
	DownloadSpeed    int64   `json:"download_speed"`
	UploadSpeed      int64   `json:"upload_speed"`
	Seeders          int     `json:"seeders"`
	DownloadPresent  bool    `json:"download_present"`
	DownloadFinished bool    `json:"download_finished"`
	Files            []File  `json:"files"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// File represents a file within a torrent
type File struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	Size              int64   `json:"size"`
	ShortName         string  `json:"short_name"`
	Mimetype          string  `json:"mimetype,omitempty"`
	OpensubtitlesHash *string `json:"opensubtitles_hash,omitempty"`
}

// TorrentInfo represents torrent information
type TorrentInfo struct {
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Seeders  int    `json:"seeders"`
	Leechers int    `json:"leechers"`
	Files    []File `json:"files"`
}

// CreateTorrentData represents the data returned when creating a torrent
type CreateTorrentData struct {
	TorrentID int    `json:"torrent_id"`
	AuthID    string `json:"auth_id"`
	Hash      string `json:"hash"`
}

// CreateTorrentResponse represents the response from creating a torrent
type CreateTorrentResponse struct {
	Success bool              `json:"success"`
	Detail  string            `json:"detail"`
	Data    CreateTorrentData `json:"data"`
}

// GetUserResponse represents the response from getting user info
type GetUserResponse struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Data    User   `json:"data"`
}

// ListTorrentsResponse represents the response from listing torrents
type ListTorrentsResponse struct {
	Success bool      `json:"success"`
	Detail  string    `json:"detail"`
	Data    []Torrent `json:"data"`
}

type ListTorrentResponse struct {
	Success bool    `json:"success"`
	Detail  string  `json:"detail"`
	Data    Torrent `json:"data"`
}

// TorrentInfoResponse represents the response from getting torrent info
type TorrentInfoResponse struct {
	Success bool        `json:"success"`
	Detail  string      `json:"detail"`
	Data    TorrentInfo `json:"data"`
}

// CachedTorrent represents a cached torrent entry
type CachedTorrent struct {
	Hash  string `json:"hash"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Files []File `json:"files,omitempty"`
}

// CheckCachedResponse represents the response from checking cached torrents
type CheckCachedResponse struct {
	Success bool                     `json:"success"`
	Detail  string                   `json:"detail"`
	Data    map[string]CachedTorrent `json:"data"`
}
