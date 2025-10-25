package realdebrid

// User represents Real-Debrid user information
type User struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Points     int    `json:"points"`
	Locale     string `json:"locale"`
	Avatar     string `json:"avatar"`
	Type       string `json:"type"`
	Premium    int    `json:"premium"`
	Expiration string `json:"expiration"`
}

// Download represents an unrestricted download link
type Download struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	MimeType   string `json:"mimeType"`
	Filesize   int64  `json:"filesize"`
	Link       string `json:"link"`
	Host       string `json:"host"`
	HostIcon   string `json:"host_icon"`
	Chunks     int    `json:"chunks"`
	CRC        int    `json:"crc"`
	Download   string `json:"download"`
	Streamable int    `json:"streamable"`
}

// TorrentInfo represents detailed information about a torrent
type TorrentInfo struct {
	ID               string        `json:"id"`
	Filename         string        `json:"filename"`
	OriginalFilename string        `json:"original_filename"`
	Hash             string        `json:"hash"`
	Bytes            int64         `json:"bytes"`
	OriginalBytes    int64         `json:"original_bytes"`
	Host             string        `json:"host"`
	Split            int           `json:"split"`
	Progress         float64       `json:"progress"`
	Status           string        `json:"status"`
	Added            string        `json:"added"`
	Files            []TorrentFile `json:"files"`
	Links            []string      `json:"links"`
	Ended            string        `json:"ended"`
	Speed            int64         `json:"speed"`
	Seeders          int           `json:"seeders"`
}

// TorrentFile represents a file within a torrent
type TorrentFile struct {
	ID       int    `json:"id"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Selected int    `json:"selected"`
}

// TorrentAddResponse represents the response when adding a torrent
type TorrentAddResponse struct {
	ID  string `json:"id"`
	URI string `json:"uri"`
}

// Host represents a supported host
type Host struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	ImageBig string `json:"image_big"`
}

// HostStatus represents the status of a host
type HostStatus struct {
	Supported int    `json:"supported"`
	Status    string `json:"status"`
	Check     string `json:"check"`
}

// Traffic represents traffic information
type Traffic struct {
	Left  int64            `json:"left"`
	Bytes int64            `json:"bytes"`
	Links int              `json:"links"`
	Limit int64            `json:"limit"`
	Type  string           `json:"type"`
	Extra int64            `json:"extra"`
	Reset string           `json:"reset"`
	Hosts map[string]int64 `json:"hosts,omitempty"`
}

// TrafficDetails represents detailed traffic information
type TrafficDetails struct {
	Host  string `json:"host"`
	Bytes int64  `json:"bytes"`
}

// StreamingTranscode represents transcoding information
type StreamingTranscode struct {
	Apple    map[string]string `json:"apple,omitempty"`
	Dash     map[string]string `json:"dash,omitempty"`
	Livemp4  map[string]string `json:"livemp4,omitempty"`
	H264webm map[string]string `json:"h264webm,omitempty"`
}

// MediaInfo represents media information for a file
type MediaInfo struct {
	Duration     string `json:"duration"`
	Bitrate      int64  `json:"bitrate"`
	Fps          string `json:"fps"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Audio        string `json:"audio"`
	AudioBitrate int    `json:"audio_bitrate"`
}

// TimeResponse represents server time response
type TimeResponse struct {
	ServerTime     int    `json:"server_time"`
	ServerTimezone string `json:"server_timezone"`
}

// TimeISOResponse represents server time in ISO format
type TimeISOResponse struct {
	ISO string `json:"iso"`
}

// CheckLinkResponse represents the response from checking a link
type CheckLinkResponse struct {
	Host      string `json:"host"`
	Link      string `json:"link"`
	Filename  string `json:"filename"`
	Filesize  int64  `json:"filesize"`
	Supported int    `json:"supported"`
}

// FolderLink represents a link in a folder
type FolderLink struct {
	Link     string `json:"link"`
	Filename string `json:"filename"`
	Filesize int64  `json:"filesize"`
	Download string `json:"download"`
}
