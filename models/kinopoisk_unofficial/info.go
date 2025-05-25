package kinopoisk_unofficial

import (
	"context"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"time"
)

type Info struct {
	tableName struct{} `pg:"kinopoisk_unofficial.info"`

	KpID      int            `pg:"kp_id,pk"`
	ImdbID    *string        `pg:"imdb_id"`
	Title     string         `pg:"title,notnull"`
	Year      *int16         `pg:"year"`
	Metadata  map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt time.Time      `pg:"created_at,default:now()"`
	UpdatedAt time.Time      `pg:"updated_at,default:now()"`
}

func GetInfoByID(ctx context.Context, db *pg.DB, kpID int) (*Info, error) {
	var info Info

	err := db.Model(&info).
		Context(ctx).
		Where("kp_id = ?", kpID).
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

func UpsertInfo(ctx context.Context, db *pg.DB, kpID int, metadata map[string]any) (*Info, error) {
	title, ok := metadata["nameEn"].(string)
	if !ok {
		title = metadata["nameOriginal"].(string)
	}
	year, ok := metadata["startYear"].(float64)
	if !ok {
		year = metadata["year"].(float64)
	}
	var err error
	var yearPtr *int16

	if year != 0 {
		y := int16(year)
		yearPtr = &y
	}

	imdbID, _ := metadata["imdbId"].(string)

	var imdbIDPtr *string

	if imdbID != "" {
		imdbIDPtr = &imdbID
	}

	m := &Info{
		KpID:     kpID,
		ImdbID:   imdbIDPtr,
		Metadata: metadata,
		Title:    title,
		Year:     yearPtr,
	}
	// Update record
	_, err = db.Model(m).
		Context(ctx).
		OnConflict("(kp_id) DO UPDATE").
		Set("metadata = EXCLUDED.metadata, title = EXCLUDED.title, year = EXCLUDED.year, imdb_id = EXCLUDED.imdb_id").
		Insert()

	return m, err
}
