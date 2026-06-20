package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/satori/go.uuid"
)

// MetadataMatchRow is one stored (parsed title → matched result title)
// link, projected for the fuzzy-match cleanup audit. ID is the movie_id
// or series_id; ResultTitle is the matched movie_metadata/series_metadata
// title. The cleanup command evaluates each row against the enrichment
// guards and detaches the false positives.
type MetadataMatchRow struct {
	ID          uuid.UUID `pg:"id"`
	ResourceID  string    `pg:"resource_id"`
	QueryTitle  string    `pg:"query_title"`
	QueryYear   *int16    `pg:"query_year"`
	ResultTitle string    `pg:"result_title"`
	VideoID     string    `pg:"video_id"`
}

// ListMovieMetadataMatches returns every movie row that carries a
// metadata link, joined to the matched movie_metadata title.
func ListMovieMetadataMatches(ctx context.Context, db *pg.DB) ([]MetadataMatchRow, error) {
	var rows []MetadataMatchRow
	err := db.ModelContext(ctx, (*Movie)(nil)).
		ColumnExpr("movie.movie_id AS id").
		ColumnExpr("movie.resource_id AS resource_id").
		ColumnExpr("movie.title AS query_title").
		ColumnExpr("movie.year AS query_year").
		ColumnExpr("mm.title AS result_title").
		ColumnExpr("mm.video_id AS video_id").
		Join("JOIN movie_metadata AS mm ON mm.movie_metadata_id = movie.movie_metadata_id").
		Select(&rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListSeriesMetadataMatches returns every series row that carries a
// metadata link, joined to the matched series_metadata title.
func ListSeriesMetadataMatches(ctx context.Context, db *pg.DB) ([]MetadataMatchRow, error) {
	var rows []MetadataMatchRow
	err := db.ModelContext(ctx, (*Series)(nil)).
		ColumnExpr("series.series_id AS id").
		ColumnExpr("series.resource_id AS resource_id").
		ColumnExpr("series.title AS query_title").
		ColumnExpr("series.year AS query_year").
		ColumnExpr("sm.title AS result_title").
		ColumnExpr("sm.video_id AS video_id").
		Join("JOIN series_metadata AS sm ON sm.series_metadata_id = series.series_metadata_id").
		Select(&rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// DetachMovieMetadata nulls movie_metadata_id for the given movie ids,
// un-enriching those resources (poster falls back to thumbnail). The
// shared movie_metadata rows are left intact — they are keyed by
// video_id and may still back a legitimate match elsewhere. Returns the
// number of rows updated.
func DetachMovieMetadata(ctx context.Context, db *pg.DB, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.ModelContext(ctx, (*Movie)(nil)).
		Set("movie_metadata_id = NULL").
		Set("updated_at = now()").
		Where("movie_id IN (?)", pg.In(ids)).
		Update()
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

// DetachSeriesMetadata nulls series_metadata_id for the given series ids.
// See DetachMovieMetadata.
func DetachSeriesMetadata(ctx context.Context, db *pg.DB, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.ModelContext(ctx, (*Series)(nil)).
		Set("series_metadata_id = NULL").
		Set("updated_at = now()").
		Where("series_id IN (?)", pg.In(ids)).
		Update()
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}
