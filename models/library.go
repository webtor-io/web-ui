package models

import (
	"context"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
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
	Name      string
}

func IsInLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, resourceID string) (bool, error) {
	exists, err := db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ?", uID, resourceID).
		Exists()
	if err != nil {
		return false, errors.Wrap(err, "failed to check library membership")
	}
	return exists, nil
}

func GetLibraryByName(ctx context.Context, db *pg.DB, uID uuid.UUID, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Where("library.user_id = ?", uID).
		Where("library.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by name")
	}
	return &lib, nil
}

func GetLibraryByTorrentName(ctx context.Context, db *pg.DB, uID uuid.UUID, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Join("join torrent_resource as t on t.resource_id = library.resource_id").
		Where("library.user_id = ?", uID).
		Where("t.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by name")
	}
	return &lib, nil
}

func AddTorrentToLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, resourceID string, info *metainfo.Info, displayName string, torrentSize int64) (*Library, error) {
	name := info.NameUtf8
	if name == "" {
		name = info.Name
	}

	filesCount := 1
	if info.Files != nil {
		filesCount = len(info.Files)
	}

	torrent := &TorrentResource{
		ResourceID:       resourceID,
		Name:             name,
		FileCount:        filesCount,
		SizeBytes:        info.TotalLength(),
		TorrentSizeBytes: torrentSize,
	}

	_, err := db.Model(torrent).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert torrent resource")
	}

	if displayName == "" {
		displayName = name
	}

	lib := &Library{
		UserID:     uID,
		ResourceID: resourceID,
		CreatedAt:  time.Now(),
		Name:       displayName,
	}

	_, err = db.Model(lib).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert library entry")
	}

	return lib, nil
}

func RemoveFromLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, rID string) error {
	_, err := db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ?", uID, rID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to remove from library")
	}
	return nil
}

func UpdateLibraryName(ctx context.Context, db *pg.DB, l *Library) error {
	_, err := db.Model(l).Context(ctx).WherePK().Column("name").Update()
	return err
}

func GetLibraryCounts(ctx context.Context, db *pg.DB, uID uuid.UUID) (torrents, movies, series int, err error) {
	torrents, err = db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ?", uID).
		Count()
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count torrents")
	}

	movies, err = db.Model((*Movie)(nil)).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Where("l.user_id = ?", uID).
		Count()
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count movies")
	}

	series, err = db.Model((*Series)(nil)).
		Context(ctx).
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Where("l.user_id = ?", uID).
		Count()
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count series")
	}

	return
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

func GetLibraryMovieTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
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
		return nil, errors.Wrap(err, "failed to fetch movie torrent list")
	}

	return list, nil
}

func GetLibrarySeriesTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join series as s").
		JoinOn("s.resource_id = library.resource_id").
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
		return nil, errors.Wrap(err, "failed to fetch movie torrent list")
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
