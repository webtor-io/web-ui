package models

import (
	"context"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
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

// String returns the i18n message key for this sort type.
// Resolved in templates via {{ t $.Lang .Title }}.
func (s SortType) String() string {
	switch s {
	case SortTypeRecentlyAdded:
		return "library.sort.recentlyAdded"
	case SortTypeName:
		return "library.sort.name"
	case SortTypeYear:
		return "library.sort.year"
	case SortTypeRating:
		return "library.sort.rating"
	default:
		return "library.sort.unknown"
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

// GetLibraryByResourceID returns the user's entry for a resource, or nil when
// there is none. Nil is "not in this user's library" and covers both the
// missing row and someone else's row — the caller must not distinguish them.
func GetLibraryByResourceID(ctx context.Context, db *pg.DB, uID uuid.UUID, resourceID string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Where("library.user_id = ?", uID).
		Where("library.resource_id = ?", resourceID).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library entry by resource id")
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

// RemoveFromLibrary removes the user's entries for every given resource in one
// statement. No resources is a no-op: pg.In renders an empty slice as "IN ()",
// which is a syntax error.
func RemoveFromLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, rIDs ...string) error {
	if len(rIDs) == 0 {
		return nil
	}
	_, err := db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id IN (?)", uID, pg.In(rIDs)).
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

func GetLibraryMovieTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
		Join("left join movie_status as ums").
		JoinOn("ums.user_id = library.user_id AND ums.video_id = mmd.video_id AND ums.watched = true").
		Where("library.user_id = ?", uID).
		Relation("Torrent")

	switch watchedFilter {
	case "unwatched":
		query.Where("ums.video_id IS NULL")
	case "watched":
		query.Where("ums.video_id IS NOT NULL")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC") // вместо torrent_resource.name
	case SortTypeYear:
		query.OrderExpr("mmd.year DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
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

func GetLibrarySeriesTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join series as s").
		JoinOn("s.resource_id = library.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("s.series_metadata_id = smd.series_metadata_id").
		Join("left join series_status as uss").
		JoinOn("uss.user_id = library.user_id AND uss.video_id = smd.video_id AND uss.watched = true").
		Where("library.user_id = ?", uID).
		Relation("Torrent")

	switch watchedFilter {
	case "unwatched":
		query.Where("uss.video_id IS NULL")
	case "watched":
		query.Where("uss.video_id IS NOT NULL")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC") // вместо torrent_resource.name
	case SortTypeYear:
		query.OrderExpr("smd.year DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("smd.rating DESC NULLS LAST")
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

// LibraryFilter narrows the movie and series lists of the library page.
// Watched is "", "unwatched" or "watched" and matches the status table's
// watched column; Genre is a genre slug (services/genre), "" for any.
type LibraryFilter struct {
	Watched string
	Genre   string
}

// where adds the filter's conditions; md and st are the aliases the query
// joins the metadata and status tables under.
func (f LibraryFilter) where(q *orm.Query, md, st string) {
	switch f.Watched {
	case "unwatched":
		q.Where(st + ".video_id IS NULL")
	case "watched":
		q.Where(st + ".video_id IS NOT NULL")
	}
	if f.Genre != "" {
		// NULL genres (not derived yet) match no genre.
		q.Where("? = ANY("+md+".genres)", f.Genre)
	}
}

// GetLibraryMovieList loads movies in the user's library, optionally filtered
// by watched state. watchedFilter is one of "", "unwatched", or "watched" and
// matches against movie_status.watched column.
func GetLibraryMovieList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string) ([]*Movie, error) {
	return GetLibraryMovieListFiltered(ctx, db, uID, sort, LibraryFilter{Watched: watchedFilter})
}

// GetLibraryMovieListFiltered is GetLibraryMovieList with every filter of
// the library page.
func GetLibraryMovieListFiltered(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, f LibraryFilter) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Join("left join movie_status as ums").
		JoinOn("ums.user_id = l.user_id AND ums.video_id = mmd.video_id AND ums.watched = true").
		Where("l.user_id = ?", uID).
		Relation("MovieMetadata").
		Relation("ResourceMetadata")

	f.where(query, "mmd", "ums")

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

// GetLibrarySeriesList is the series counterpart to GetLibraryMovieList.
// Filtering considers only series-level series_status (manual declaration
// or auto_all_episodes); per-episode rows are not included in the filter.
func GetLibrarySeriesList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string) ([]*Series, error) {
	return GetLibrarySeriesListFiltered(ctx, db, uID, sort, LibraryFilter{Watched: watchedFilter})
}

// GetLibrarySeriesListFiltered is GetLibrarySeriesList with every filter of
// the library page.
func GetLibrarySeriesListFiltered(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, f LibraryFilter) ([]*Series, error) {
	var list []*Series

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Join("left join series_status as uss").
		JoinOn("uss.user_id = l.user_id AND uss.video_id = smd.video_id AND uss.watched = true").
		Where("l.user_id = ?", uID).
		Relation("SeriesMetadata").
		Relation("ResourceMetadata")

	f.where(query, "smd", "uss")

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

// GetLibraryGenres returns the distinct genre slugs of the user's movies or
// series, in no particular order: the options of the library's genre filter.
func GetLibraryGenres(ctx context.Context, db *pg.DB, uID uuid.UUID, ct ContentType) ([]string, error) {
	var query *orm.Query
	switch ct {
	case ContentTypeMovie:
		query = db.Model((*Movie)(nil)).
			Join("join movie_metadata as md").
			JoinOn("movie.movie_metadata_id = md.movie_metadata_id").
			Join("join library as l").
			JoinOn("movie.resource_id = l.resource_id")
	case ContentTypeSeries:
		query = db.Model((*Series)(nil)).
			Join("join series_metadata as md").
			JoinOn("series.series_metadata_id = md.series_metadata_id").
			Join("join library as l").
			JoinOn("series.resource_id = l.resource_id")
	default:
		return nil, errors.Errorf("no library genres for content type %q", ct)
	}
	var genres []string
	err := query.
		Context(ctx).
		ColumnExpr("DISTINCT unnest(md.genres)").
		Where("l.user_id = ?", uID).
		Select(&genres)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library genres")
	}
	return genres, nil
}
