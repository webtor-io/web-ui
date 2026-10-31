package models

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// The genre filter and its options, against the real schema: the filter is
// a text[] membership test in SQL, and NULL genres (not derived yet) must
// neither match nor show up as an option.
func TestLibraryGenreFilter(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()
	owner := createTestUser(t, db, "owner@example.com")
	other := createTestUser(t, db, "other@example.com")

	// add puts one title in a user's library: media_info, the metadata row,
	// the movie or series row and the library row.
	add := func(u uuid.UUID, table, resourceID, videoID string, year int, genres []string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO media_info (resource_id, status) VALUES (?, 1) ON CONFLICT DO NOTHING`, resourceID); err != nil {
			t.Fatal(err)
		}
		var mdID uuid.UUID
		if _, err := db.QueryOneContext(ctx, pg.Scan(&mdID), `INSERT INTO ? (video_id, title, year, genres) VALUES (?, ?, ?, ?) RETURNING ?`,
			pg.Ident(table+"_metadata"), videoID, videoID, year, pg.Array(genres), pg.Ident(table+"_metadata_id")); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO ? (resource_id, title, ?) VALUES (?, ?, ?)`,
			pg.Ident(table), pg.Ident(table+"_metadata_id"), resourceID, videoID, mdID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO library (user_id, resource_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, u, resourceID); err != nil {
			t.Fatal(err)
		}
	}
	add(owner, "movie", "r1", "tt1", 2001, []string{"drama", "war"})
	add(owner, "movie", "r2", "tt2", 2002, []string{"comedy", "drama"})
	add(owner, "movie", "r3", "tt3", 2003, nil)        // not derived yet
	add(owner, "movie", "r4", "tt4", 2004, []string{}) // TMDB lists none
	add(other, "movie", "r5", "tt5", 2005, []string{"horror", "drama"})
	add(owner, "series", "s1", "tts1", 2011, []string{"scifi", "fantasy"})
	add(owner, "series", "s2", "tts2", 2012, []string{"drama"})
	if _, err := db.ExecContext(ctx, `INSERT INTO movie_status (user_id, video_id, watched, source) VALUES (?, 'tt2', true, 1)`, owner); err != nil {
		t.Fatal(err)
	}

	movies := func(f LibraryFilter) []string {
		t.Helper()
		ls, err := GetLibraryMovieListFiltered(ctx, db, owner, SortTypeYear, f)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, m := range ls {
			ids = append(ids, m.MovieMetadata.VideoID)
		}
		return ids
	}
	for name, c := range map[string]struct {
		f    LibraryFilter
		want []string
	}{
		"no filter, year desc":   {LibraryFilter{}, []string{"tt4", "tt3", "tt2", "tt1"}},
		"genre":                  {LibraryFilter{Genre: "drama"}, []string{"tt2", "tt1"}},
		"genre of one title":     {LibraryFilter{Genre: "war"}, []string{"tt1"}},
		"genre and unwatched":    {LibraryFilter{Genre: "drama", Watched: "unwatched"}, []string{"tt1"}},
		"genre and watched":      {LibraryFilter{Genre: "drama", Watched: "watched"}, []string{"tt2"}},
		"other user's genre":     {LibraryFilter{Genre: "horror"}, nil},
		"genre nobody has":       {LibraryFilter{Genre: "western"}, nil},
		"watched without genres": {LibraryFilter{Watched: "unwatched"}, []string{"tt4", "tt3", "tt1"}},
	} {
		if got := movies(c.f); !reflect.DeepEqual(got, c.want) {
			t.Errorf("movies, %s: got %v, want %v", name, got, c.want)
		}
	}

	ss, err := GetLibrarySeriesListFiltered(ctx, db, owner, SortTypeYear, LibraryFilter{Genre: "fantasy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].SeriesMetadata.VideoID != "tts1" {
		t.Errorf("series, fantasy: got %+v, want tts1 only", ss)
	}

	for _, c := range []struct {
		ct   ContentType
		want []string
	}{
		{ContentTypeMovie, []string{"comedy", "drama", "war"}},
		{ContentTypeSeries, []string{"drama", "fantasy", "scifi"}},
	} {
		got, err := GetLibraryGenres(ctx, db, owner, c.ct)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s genres: got %v, want %v", c.ct, got, c.want)
		}
	}
	if _, err := GetLibraryGenres(ctx, db, owner, ContentType("episode")); err == nil {
		t.Error("an unknown content type must be an error, not every genre")
	}
}
