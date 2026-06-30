package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
)

type Episode struct {
	tableName struct{} `pg:"episode"`

	EpisodeID         uuid.UUID      `pg:"episode_id,pk,type:uuid,default:uuid_generate_v4()"`
	SeriesID          uuid.UUID      `pg:"series_id"`
	Season            *int16         `pg:"season"`
	Episode           *int16         `pg:"episode"`
	ResourceID        string         `pg:"resource_id"`
	Title             *string        `pg:"title"`
	Path              *string        `pg:"path"`
	FileIdx           *int           `pg:"file_idx"`
	FileSize          *int64         `pg:"file_size"`
	Metadata          map[string]any `pg:"metadata,type:jsonb"`
	EpisodeMetadataID *uuid.UUID     `pg:"episode_metadata_id"`
	CreatedAt         time.Time      `pg:"created_at,default:now()"`
	UpdatedAt         time.Time      `pg:"updated_at,default:now()"`

	Series          *Series          `pg:"rel:has-one,fk:series_id"`
	MediaInfo       *MediaInfo       `pg:"rel:has-one,fk:resource_id"`
	EpisodeMetadata *EpisodeMetadata `pg:"rel:has-one,fk:episode_metadata_id"`
}

// GetFirstEpisodePathForSeries returns one representative episode filename
// for a series — used by the AI enrichment fallback to feed Claude an
// actual torrent path (Series rows themselves don't carry one). Returns
// "" when the series has no episodes with a non-null path.
func GetFirstEpisodePathForSeries(ctx context.Context, db *pg.DB, seriesID uuid.UUID) (string, error) {
	var ep Episode
	err := db.Model(&ep).
		Context(ctx).
		Column("path").
		Where("series_id = ?", seriesID).
		Where("path IS NOT NULL").
		OrderExpr("season ASC NULLS LAST, episode ASC NULLS LAST").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if ep.Path == nil {
		return "", nil
	}
	return *ep.Path, nil
}
