package models

import (
	"time"

	"github.com/satori/go.uuid"
)

type Episode struct {
	tableName struct{} `pg:"episode"`

	SeriesID   uuid.UUID      `pg:"series_id,pk"`
	Season     int16          `pg:"season,pk"`
	Episode    int16          `pg:"episode,pk"`
	ResourceID string         `pg:"resource_id"`
	Title      *string        `pg:"title"`
	Path       *string        `pg:"path"`
	Metadata   map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt  time.Time      `pg:"created_at,default:now()"`
	UpdatedAt  time.Time      `pg:"updated_at,default:now()"`

	Series    *Series    `pg:"rel:has-one,fk:series_id"`
	MediaInfo *MediaInfo `pg:"rel:has-one,fk:resource_id"`
}
