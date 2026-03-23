package tmdb

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type SeasonInfo struct {
	tableName struct{} `pg:"tmdb.season_info"`

	TmdbID    int            `pg:"tmdb_id,pk"`
	Season    int16          `pg:"season,pk"`
	Metadata  map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt time.Time      `pg:"created_at,default:now()"`
	UpdatedAt time.Time      `pg:"updated_at,default:now()"`
}

func GetSeasonInfo(ctx context.Context, db *pg.DB, tmdbID int, season int16) (*SeasonInfo, error) {
	var info SeasonInfo

	err := db.Model(&info).
		Context(ctx).
		Where("tmdb_id = ?", tmdbID).
		Where("season = ?", season).
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

func UpsertSeasonInfo(ctx context.Context, db *pg.DB, tmdbID int, season int16, metadata map[string]any) (*SeasonInfo, error) {
	m := &SeasonInfo{
		TmdbID:   tmdbID,
		Season:   season,
		Metadata: metadata,
	}

	_, err := db.Model(m).
		Context(ctx).
		OnConflict("(tmdb_id, season) DO UPDATE").
		Set("metadata = EXCLUDED.metadata").
		Insert()

	return m, err
}
