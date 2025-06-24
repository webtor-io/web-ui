package models

import (
	"context"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"time"
)

type SortType int

const (
	SortTypeRecentlyAdded SortType = iota
	SortTypeName
	SortTypeYear
	SortTypeRating
)

func (s SortType) String() string {
	switch s {
	case SortTypeRecentlyAdded:
		return "Recently Added"
	case SortTypeName:
		return "Name (A-Z)"
	case SortTypeYear:
		return "Year"
	case SortTypeRating:
		return "Rating"
	default:
		return "Unknown"
	}
}

type Library struct {
	tableName struct{} `pg:"library"`

	UserID     uuid.UUID `pg:"user_id,pk"`
	ResourceID string    `pg:"resource_id,pk"`
	CreatedAt  time.Time `pg:"created_at"`

	Torrent   *TorrentResource `pg:"rel:has-one,fk:resource_id"`
	MediaInfo *MediaInfo       `pg:"rel:has-one,fk:resource_id"`
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

func GetLibraryTorrentsList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Where("library.user_id = ?", uID).
		Relation("Torrent")

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC") // вместо torrent_resource.name
	case SortTypeRecentlyAdded:
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

func GetMovieByID(ctx context.Context, db *pg.DB, uID uuid.UUID, movieID string) (*Movie, error) {
	var m Movie

	query := db.Model(&m).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Where("movie.movie_id = ?", movieID).
		Where("l.user_id = ?", uID).
		Limit(1)

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie")
	}

	return &m, nil
}

func GetMoviesByVideoID(ctx context.Context, db *pg.DB, uID uuid.UUID, videoID string) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Where("l.user_id = ?", uID).
		Where("mmd.video_id = ?", videoID).
		Relation("MovieMetadata")

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie list")
	}

	return list, nil
}

func GetLibraryMovieList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Where("l.user_id = ?", uID).
		Relation("MovieMetadata")

	switch sort {
	case SortTypeRecentlyAdded:
		query.OrderExpr("l.created_at DESC")
	case SortTypeName:
		query.OrderExpr("mmd.title ASC")
	case SortTypeYear:
		query.OrderExpr("mmd.year DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie list")
	}

	return list, nil
}

func GetLibrarySeriesList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType) ([]*Series, error) {
	var list []*Series

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Where("l.user_id = ?", uID).
		Relation("SeriesMetadata")

	switch sort {
	case SortTypeRecentlyAdded:
		query.OrderExpr("l.created_at DESC")
	case SortTypeName:
		query.OrderExpr("smd.title ASC")
	case SortTypeYear:
		query.OrderExpr("smd.year DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("smd.rating DESC NULLS LAST")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch series list")
	}

	return list, nil
}
