package models

type VideoMetadata struct {
	VideoID   string   `pg:"video_id,unique"`
	Title     string   `pg:"title"`
	Year      *int16   `pg:"year"`
	Plot      string   `pg:"plot"`
	PosterURL string   `pg:"poster_url"`
	Rating    *float64 `pg:"rating"`
	// Genres are services/genre slugs. nil is written as NULL, "not derived"
	// (a mapper without genre data); an empty slice as '{}', "derived, none".
	Genres []string `pg:"genres,array"`
}
