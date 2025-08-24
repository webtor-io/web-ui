package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
)

type TorrentResource struct {
	tableName struct{} `pg:"torrent_resource"`

	ResourceID       string    `pg:"resource_id,pk"`
	Name             string    `pg:"name"`
	FileCount        int       `pg:"file_count"`
	SizeBytes        int64     `pg:"size_bytes"`
	CreatedAt        time.Time `pg:"created_at"`
	TorrentSizeBytes int64     `pg:"torrent_size_bytes"`

	LibraryEntries []*Library `pg:"rel:has-many,fk:resource_id"`
	MediaInfo      *MediaInfo `pg:"rel:has-one,fk:resource_id"`
}

func GetResourcesWithoutMediaInfo(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).
		Context(ctx).
		Where("media_info.resource_id IS NULL").
		Join("LEFT JOIN media_info ON media_info.resource_id = torrent_resource.resource_id").
		Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetAllResources(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).Context(ctx).Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetErrorResources(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).
		Context(ctx).
		Join("JOIN media_info ON media_info.resource_id = torrent_resource.resource_id").
		Where("media_info.status in (?)", pg.In([]int16{
			int16(MediaInfoStatusProcessing),
			int16(MediaInfoStatusError),
		})).
		Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetResourceByID(ctx context.Context, db *pg.DB, id string) (*TorrentResource, error) {
	var resource TorrentResource
	err := db.Model(&resource).Context(ctx).Where("resource_id = ?", id).Limit(1).Select()

	if err != nil {
		return nil, err
	}

	return &resource, nil
}
