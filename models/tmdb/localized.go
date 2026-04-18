package tmdb

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type Localized struct {
	tableName struct{} `pg:"tmdb.localized"`

	TmdbID    int       `pg:"tmdb_id,pk"`
	Lang      string    `pg:"lang,pk"`
	Title     string    `pg:"title,notnull"`
	Plot      string    `pg:"plot,notnull"`
	CreatedAt time.Time `pg:"created_at,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`
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

func UpsertLocalized(ctx context.Context, db *pg.DB, tmdbID int, lang string, title string, plot string) (*Localized, error) {
	m := &Localized{
		TmdbID: tmdbID,
		Lang:   lang,
		Title:  title,
		Plot:   plot,
	}

	_, err := db.Model(m).
		Context(ctx).
		OnConflict("(tmdb_id, lang) DO UPDATE").
		Set("title = EXCLUDED.title, plot = EXCLUDED.plot").
		Insert()

	return m, err
}
