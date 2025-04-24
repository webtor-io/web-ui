package models

import "time"

type TorrentResource struct {
	tableName struct{} `pg:"torrent_resource"`

	ResourceID string    `pg:"resource_id,pk"`
	Name       string    `pg:"name"`
	FileCount  int       `pg:"file_count"`
	SizeBytes  int64     `pg:"size_bytes"`
	CreatedAt  time.Time `pg:"created_at"`

	LibraryEntries []*Library `pg:"rel:has-many,fk:resource_id"`
}
