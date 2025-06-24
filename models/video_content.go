package models

import uuid "github.com/satori/go.uuid"

type VideoContent struct {
	ResourceID string         `pg:"resource_id"`
	Title      string         `pg:"title"`
	Year       *int16         `pg:"year"`
	Metadata   map[string]any `pg:"metadata,type:jsonb"`
}

type ContentType string

const (
	ContentTypeMovie  ContentType = "movie"
	ContentTypeSeries ContentType = "series"
)

type VideoContentWithMetadata interface {
	GetID() uuid.UUID
	GetContentType() ContentType
	GetContent() *VideoContent
	GetMetadata() *VideoMetadata
	GetPath() *string
	GetEpisode(season int, episode int) *Episode
}
