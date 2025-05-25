package omdb

import (
	"context"
	"errors"
	"github.com/go-pg/pg/v10"
	"regexp"
	"strconv"
	"time"
)

type Info struct {
	tableName struct{} `pg:"omdb.info"`

	ImdbID    string         `pg:"imdb_id,pk"`
	Title     string         `pg:"title,notnull"`
	Year      *int16         `pg:"year"` // nullable
	Type      OmdbType       `pg:"type"`
	Metadata  map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt time.Time      `pg:"created_at,default:now()"`
	UpdatedAt time.Time      `pg:"updated_at,default:now()"`
}

type OmdbType int16

const (
	OmdbTypeMovie   OmdbType = 1
	OmdbTypeSeries  OmdbType = 2
	OmdbTypeEpisode OmdbType = 3
)

var startYearRegexp = regexp.MustCompile(`^\d{4}`)

func UpsertInfo(ctx context.Context, db *pg.DB, imdbID string, omdbType OmdbType, metadata map[string]any) (*Info, error) {

	title := metadata["Title"].(string)
	yearStr := metadata["Year"].(string)
	var year int
	var err error
	if yearStr != "" {
		yearMatches := startYearRegexp.FindStringSubmatch(yearStr)
		year, err = strconv.Atoi(yearMatches[0])
		if err != nil {
			return nil, err
		}
	}

	var yearPtr *int16

	if year != 0 {
		y := int16(year)
		yearPtr = &y
	}

	m := &Info{
		ImdbID:   imdbID,
		Type:     omdbType,
		Metadata: metadata,
		Title:    title,
		Year:     yearPtr,
	}
	// Update record
	_, err = db.Model(m).
		Context(ctx).
		OnConflict("(imdb_id) DO UPDATE").
		Set("type = EXCLUDED.type, metadata = EXCLUDED.metadata, title = EXCLUDED.title, year = EXCLUDED.year").
		Insert()

	return m, err
}

func GetInfoByID(ctx context.Context, db *pg.DB, imdbID string) (*Info, error) {
	var info Info

	err := db.Model(&info).
		Context(ctx).
		Where("imdb_id = ?", imdbID).
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
