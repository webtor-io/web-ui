package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// MetadataGenres is a movie_metadata / series_metadata row as the genres
// backfill reads and writes it.
type MetadataGenres struct {
	ID      uuid.UUID `pg:"id"`
	VideoID string    `pg:"video_id"`
	Genres  []string  `pg:"genres,array"`
}

// genresTable names the metadata table of a content type and its key.
func genresTable(ct ContentType) (table, pk string) {
	if ct == ContentTypeSeries {
		return "series_metadata", "series_metadata_id"
	}
	return "movie_metadata", "movie_metadata_id"
}

// ListMetadataWithoutGenres returns up to limit rows of the content type's
// metadata table whose genres are not derived yet (NULL).
//
// ponytail: each call scans from the start of the table, so a first run over
// N rows costs N/batch scans (~115 for the 57k movie rows of 2026-06); page
// by pk if that ever shows up.
func ListMetadataWithoutGenres(ctx context.Context, db *pg.DB, ct ContentType, limit int) ([]MetadataGenres, error) {
	table, pk := genresTable(ct)
	var rows []MetadataGenres
	_, err := db.QueryContext(ctx, &rows,
		`SELECT ? AS id, video_id FROM public.? WHERE genres IS NULL LIMIT ?`,
		pg.Ident(pk), pg.Ident(table), limit)
	return rows, err
}

// SetMetadataGenres writes the genres of rows in one statement and returns
// how many it wrote. Only rows still NULL are written: one an enrichment
// filled in the meantime keeps that newer value.
func SetMetadataGenres(ctx context.Context, db *pg.DB, ct ContentType, rows []MetadataGenres) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	table, pk := genresTable(ct)
	values := make([]any, len(rows))
	for i, r := range rows {
		values[i] = []any{r.ID.String(), pg.Array(r.Genres)}
	}
	res, err := db.ExecContext(ctx, `
		UPDATE public.? AS m SET genres = d.genres::text[]
		FROM (VALUES ?) AS d(id, genres)
		WHERE m.? = d.id::uuid AND m.genres IS NULL`,
		pg.Ident(table), pg.In(values), pg.Ident(pk))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}
