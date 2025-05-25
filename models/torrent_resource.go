package models

import (
	"context"
	"github.com/go-pg/pg/v10"
	"time"
)

type TorrentResource struct {
	tableName struct{} `pg:"torrent_resource"`

	ResourceID string    `pg:"resource_id,pk"`
	Name       string    `pg:"name"`
	FileCount  int       `pg:"file_count"`
	SizeBytes  int64     `pg:"size_bytes"`
	CreatedAt  time.Time `pg:"created_at"`

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

func GetResourceByID(ctx context.Context, db *pg.DB, id string) (*TorrentResource, error) {
	var resource TorrentResource
	err := db.Model(&resource).Context(ctx).Where("resource_id = ?", id).Limit(1).Select()

	if err != nil {
		return nil, err
	}

	return &resource, nil
}
