package models

import (
	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"time"
)

type Library struct {
	tableName struct{} `pg:"library"`

	UserID     uuid.UUID `pg:"user_id,pk"`
	ResourceID string    `pg:"resource_id,pk"`
	CreatedAt  time.Time `pg:"created_at"`

	Torrent *TorrentResource `pg:"rel:has-one,fk:resource_id"`
}

func IsInLibrary(db *pg.DB, uID uuid.UUID, resourceID string) (bool, error) {
	exists, err := db.Model((*Library)(nil)).
		Where("user_id = ? AND resource_id = ?", uID, resourceID).
		Exists()
	if err != nil {
		return false, errors.Wrap(err, "failed to check library membership")
	}
	return exists, nil
}

func AddTorrentToLibrary(db *pg.DB, uID uuid.UUID, resourceID string, info metainfo.Info) error {
	name := info.NameUtf8
	if name == "" {
		name = info.Name
	}

	filesCount := 1
	if info.Files != nil {
		filesCount = len(info.Files)
	}

	torrent := &TorrentResource{
		ResourceID: resourceID,
		Name:       name,
		FileCount:  filesCount,
		SizeBytes:  info.TotalLength(),
	}

	_, err := db.Model(torrent).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to insert torrent resource")
	}

	lib := &Library{
		UserID:     uID,
		ResourceID: resourceID,
		CreatedAt:  time.Now(),
	}

	_, err = db.Model(lib).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to insert library entry")
	}

	return nil
}

func RemoveFromLibrary(db *pg.DB, uID uuid.UUID, rID string) error {
	_, err := db.Model((*Library)(nil)).
		Where("user_id = ? AND resource_id = ?", uID, rID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to remove from library")
	}
	return nil
}

type LibrarySort int

const (
	SortByNewest LibrarySort = iota
	SortByName
)

func GetLibraryList(db *pg.DB, uID uuid.UUID, sort LibrarySort) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Where("library.user_id = ?", uID).
		Relation("Torrent")

	switch sort {
	case SortByName:
		query.OrderExpr("torrent.name ASC") // вместо torrent_resource.name
	case SortByNewest:
		fallthrough
	default:
		query.OrderExpr("library.created_at DESC")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library list")
	}

	return list, nil
}
