package libapi

import (
	"time"

	"github.com/webtor-io/web-ui/models"
)

// Library filters. They map onto the same queries the library UI's tabs use
// (models.GetLibrary*TorrentList), so "movies" here means exactly what the
// Movies tab shows — a torrent with at least one recognized movie — rather than
// a second opinion derived from media_type.
const (
	LibraryTypeAll    = "all"
	LibraryTypeMovies = "movies"
	LibraryTypeSeries = "series"
)

// Library sort orders, mirroring the UI's.
const (
	LibrarySortRecent = "recent"
	LibrarySortName   = "name"
	// LibrarySortYear orders by release year (movies and series sections
	// only — a bare torrent has no year).
	LibrarySortYear = "year"
)

// LibraryItem is one torrent in the user's library.
type LibraryItem struct {
	// ResourceID is the infohash, and the id every /resource endpoint takes.
	ResourceID string `json:"resource_id" example:"08ada5a7a6183aae1e09d831df6748d566095a10"`
	// Name is the library name, which the user may have renamed; it starts as
	// the torrent name.
	Name       string    `json:"name" example:"Sintel"`
	Size       int64     `json:"size" example:"734003200"`
	FilesCount int       `json:"files_count" example:"3"`
	AddedAt    time.Time `json:"added_at" example:"2026-01-02T15:04:05Z"`
}

// LibraryListResponse pages the library. The field names follow rest-api's
// ListResponse (`items` / `items_count`) so a client handles both the same way.
type LibraryListResponse struct {
	Items []LibraryItem `json:"items"`
	// Count is the total number of matching items, not the size of this page.
	Count  int    `json:"items_count" example:"42"`
	Limit  int    `json:"limit" example:"100"`
	Offset int    `json:"offset" example:"0"`
	Type   string `json:"type" example:"all"`
	Sort   string `json:"sort" example:"recent"`
}

// LibraryAddRequest adds an existing resource to the library. The resource has
// to be in the store already — POST /resource puts it there, and returns the id
// to use here.
type LibraryAddRequest struct {
	ResourceID string `json:"resource_id" binding:"required" example:"08ada5a7a6183aae1e09d831df6748d566095a10"`
}

// LibraryRenameRequest renames a library entry. It changes the name everywhere
// the item is shown — the library UI, WebDAV and S3 — because they all read the
// same row.
type LibraryRenameRequest struct {
	Name string `json:"name" binding:"required" example:"Sintel (2010)"`
}

// NewLibraryItem flattens a library row and its torrent into the wire shape.
// The torrent relation may be missing on a row written before enrichment ran;
// the zero sizes are honest in that case, and the id is what a client acts on.
func NewLibraryItem(l *models.Library) LibraryItem {
	item := LibraryItem{
		ResourceID: l.ResourceID,
		Name:       l.Name,
		AddedAt:    l.CreatedAt,
	}
	if l.Torrent != nil {
		if item.Name == "" {
			item.Name = l.Torrent.Name
		}
		item.Size = l.Torrent.SizeBytes
		item.FilesCount = l.Torrent.FileCount
	}
	return item
}
