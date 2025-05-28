package models

import (
	"time"

	"github.com/satori/go.uuid"
)

type Episode struct {
	tableName struct{} `pg:"episode"`

	EpisodeID  uuid.UUID      `pg:"episode_id,pk,type:uuid,default:uuid_generate_v4()"`
	SeriesID   uuid.UUID      `pg:"series_id"`
	Season     *int16         `pg:"season"`
	Episode    *int16         `pg:"episode"`
	ResourceID string         `pg:"resource_id"`
	Title      *string        `pg:"title"`
	Path       *string        `pg:"path"`
	Metadata   map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt  time.Time      `pg:"created_at,default:now()"`
	UpdatedAt  time.Time      `pg:"updated_at,default:now()"`

	Series    *Series    `pg:"rel:has-one,fk:series_id"`
	MediaInfo *MediaInfo `pg:"rel:has-one,fk:resource_id"`
}
