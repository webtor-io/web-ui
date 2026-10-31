package tmdb

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type Localized struct {
	tableName struct{} `pg:"tmdb.localized"`

	TmdbID int    `pg:"tmdb_id,pk"`
	Lang   string `pg:"lang,pk"`
	Title  string `pg:"title,notnull"`
	Plot   string `pg:"plot,notnull"`
	// Names maps a TMDB person id (decimal string) to the person's name in
	// Lang. nil is a row stored before names were (NULL), empty is
	// "fetched, nothing to store" ('{}'): go-pg writes a nil map as NULL
	// and reads NULL back as nil.
	Names     map[string]string `pg:"names,type:jsonb"`
	CreatedAt time.Time         `pg:"created_at,default:now()"`
	UpdatedAt time.Time         `pg:"updated_at,default:now()"`
}

func GetLocalized(ctx context.Context, db *pg.DB, tmdbID int, lang string) (*Localized, error) {
	var loc Localized

	err := db.Model(&loc).
		Context(ctx).
		Where("tmdb_id = ?", tmdbID).
		Where("lang = ?", lang).
		Limit(1).
		Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &loc, nil
}

func UpsertLocalized(ctx context.Context, db *pg.DB, tmdbID int, lang string, title string, plot string, names map[string]string) (*Localized, error) {
	m := &Localized{
		TmdbID: tmdbID,
		Lang:   lang,
		Title:  title,
		Plot:   plot,
		Names:  names,
	}

	_, err := db.Model(m).
		Context(ctx).
		OnConflict("(tmdb_id, lang) DO UPDATE").
		Set("title = EXCLUDED.title, plot = EXCLUDED.plot, names = EXCLUDED.names").
		Insert()

	return m, err
}

// MarkLocalizedEmpty records that TMDB had nothing usable for the title in
// lang (404, or neither title nor plot): a row with no title or plot and
// names '{}', so it reads as fetched and is never fetched again. A row
// already there keeps its title, plot and names; only NULL names become
// '{}'. Raw SQL because go-pg writes an empty title as DEFAULT, and title
// has none.
func MarkLocalizedEmpty(ctx context.Context, db *pg.DB, tmdbID int, lang string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tmdb.localized (tmdb_id, lang, title, names) VALUES (?, ?, '', '{}')
		ON CONFLICT (tmdb_id, lang) DO UPDATE SET names = '{}' WHERE tmdb.localized.names IS NULL`,
		tmdbID, lang)
	return err
}
