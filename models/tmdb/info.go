package tmdb

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type TmdbType int16

const (
	TmdbTypeMovie  TmdbType = 1
	TmdbTypeSeries TmdbType = 2
)

type Info struct {
	tableName struct{} `pg:"tmdb.info"`

	TmdbID    int            `pg:"tmdb_id,pk"`
	ImdbID    *string        `pg:"imdb_id"`
	Title     string         `pg:"title,notnull"`
	Year      *int16         `pg:"year"`
	Type      TmdbType       `pg:"type,notnull"`
	Metadata  map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt time.Time      `pg:"created_at,default:now()"`
	UpdatedAt time.Time      `pg:"updated_at,default:now()"`
}

func GetInfoByID(ctx context.Context, db *pg.DB, tmdbID int) (*Info, error) {
	var info Info

	err := db.Model(&info).
		Context(ctx).
		Where("tmdb_id = ?", tmdbID).
		Limit(1).
		Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &info, nil
}

func UpsertInfo(ctx context.Context, db *pg.DB, tmdbID int, tmdbType TmdbType, metadata map[string]any) (*Info, error) {
	var title string
	if t, ok := metadata["title"].(string); ok && t != "" {
		title = t
	} else if n, ok := metadata["name"].(string); ok && n != "" {
		title = n
	}

	var yearPtr *int16
	if rd, ok := metadata["release_date"].(string); ok && len(rd) >= 4 {
		y := parseYear(rd)
		if y != 0 {
			yy := int16(y)
			yearPtr = &yy
		}
	} else if fad, ok := metadata["first_air_date"].(string); ok && len(fad) >= 4 {
		y := parseYear(fad)
		if y != 0 {
			yy := int16(y)
			yearPtr = &yy
		}
	}

	var imdbID *string
	if id, ok := metadata["imdb_id"].(string); ok && id != "" {
		imdbID = &id
	}

	m := &Info{
		TmdbID:   tmdbID,
		ImdbID:   imdbID,
		Title:    title,
		Year:     yearPtr,
		Type:     tmdbType,
		Metadata: metadata,
	}

	_, err := db.Model(m).
		Context(ctx).
		OnConflict("(tmdb_id) DO UPDATE").
		Set("type = EXCLUDED.type, metadata = EXCLUDED.metadata, title = EXCLUDED.title, year = EXCLUDED.year, imdb_id = EXCLUDED.imdb_id").
		Insert()

	return m, err
}

func parseYear(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		c := dateStr[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}
