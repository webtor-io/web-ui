package enrich

import (
	"reflect"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

func tmdbGenres(ids ...float64) []any {
	var out []any
	for _, id := range ids {
		out = append(out, map[string]any{"id": id, "name": "x"})
	}
	return out
}

func TestGenresFor(t *testing.T) {
	imdb := func(s string) *string { return &s }
	infos := []tm.Genres{ // lowest tmdb_id first, as ListGenres orders them
		{TmdbID: 1, ImdbID: imdb("tt1"), Type: tm.TmdbTypeSeries, Genres: tmdbGenres(10759, 18)},
		{TmdbID: 2, ImdbID: imdb("tt1"), Type: tm.TmdbTypeMovie, Genres: tmdbGenres(28)},
		{TmdbID: 3, ImdbID: imdb("tt3"), Type: tm.TmdbTypeSeries, Genres: tmdbGenres(10765, 10768)},
		{TmdbID: 7, Type: tm.TmdbTypeSeries, Genres: tmdbGenres(35)},
		{TmdbID: 8, Type: tm.TmdbTypeMovie, Genres: nil},
	}
	rows := func(ids ...string) []models.MetadataGenres {
		var out []models.MetadataGenres
		for _, id := range ids {
			out = append(out, models.MetadataGenres{ID: uuid.NewV4(), VideoID: id})
		}
		return out
	}
	for _, c := range []struct {
		name string
		ct   models.ContentType
		ids  []string
		want [][]string
	}{
		{"tt prefers the table's type", models.ContentTypeMovie, []string{"tt1"}, [][]string{{"action"}}},
		{"tt prefers the table's type, series", models.ContentTypeSeries, []string{"tt1"}, [][]string{{"action", "adventure", "drama"}}},
		{"tt of the other type only is still that title", models.ContentTypeMovie, []string{"tt3"}, [][]string{{"scifi", "fantasy", "war", "politics"}}},
		{"tt not cached", models.ContentTypeMovie, []string{"tt404"}, [][]string{{}}},
		{"tmdbN of the table's type", models.ContentTypeSeries, []string{"tmdb7"}, [][]string{{"comedy"}}},
		{"tmdbN of the other type is another title", models.ContentTypeMovie, []string{"tmdb7"}, [][]string{{}}},
		{"tmdbN without genres", models.ContentTypeMovie, []string{"tmdb8"}, [][]string{{}}},
		{"kp and junk", models.ContentTypeMovie, []string{"kp123", "tmdbx", ""}, [][]string{{}, {}, {}}},
	} {
		in := rows(c.ids...)
		got := genresFor(in, c.ct, infos)
		if len(got) != len(in) {
			t.Fatalf("%s: %d rows back for %d", c.name, len(got), len(in))
		}
		for i, r := range got {
			if r.ID != in[i].ID || r.VideoID != in[i].VideoID {
				t.Errorf("%s: row %d came back as another row", c.name, i)
			}
			if r.Genres == nil {
				t.Errorf("%s: %s: nil genres would be written back as NULL and selected again", c.name, r.VideoID)
			}
			if !reflect.DeepEqual(r.Genres, c.want[i]) {
				t.Errorf("%s: %s: got %v, want %v", c.name, r.VideoID, r.Genres, c.want[i])
			}
		}
	}
}

// Every writer of *_metadata goes through makeVideoMetadata, so this is where
// a TMDB title gets its genres, and "TMDB lists none" must not become NULL.
func TestMakeVideoMetadataGenres(t *testing.T) {
	s := &TMDB{}
	md := s.makeVideoMetadata(&tm.Info{TmdbID: 1, Metadata: map[string]any{"genres": tmdbGenres(18, 9648)}})
	if !reflect.DeepEqual(md.Genres, []string{"drama", "mystery"}) {
		t.Errorf("got %v", md.Genres)
	}
	md = s.makeVideoMetadata(&tm.Info{TmdbID: 1, Metadata: map[string]any{}})
	if md.Genres == nil || len(md.Genres) != 0 {
		t.Errorf("no genres: got %#v, want an empty non-nil slice", md.Genres)
	}
}
