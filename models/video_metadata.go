package models

type VideoMetadata struct {
	VideoID   string   `pg:"video_id,unique"`
	Title     string   `pg:"title"`
	Year      *int16   `pg:"year"`
	Plot      string   `pg:"plot"`
	PosterURL string   `pg:"poster_url"`
	Rating    *float64 `pg:"rating"`
}
