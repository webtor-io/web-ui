package models

type VideoContent struct {
	Title    string         `pg:"title"`
	Year     *int16         `pg:"year"`
	Metadata map[string]any `pg:"metadata,type:jsonb"`
}

type ContentType string

const (
	ContentTypeMovie  ContentType = "movie"
	ContentTypeSeries ContentType = "series"
)

type VideoContentWithMetadata interface {
	GetContentType() ContentType
	GetContent() *VideoContent
	GetMetadata() *VideoMetadata
}
