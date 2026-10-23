package enrich

import (
	"reflect"
	"testing"

	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

func person(name string, kv ...string) map[string]any {
	p := map[string]any{"name": name}
	for i := 0; i+1 < len(kv); i += 2 {
		p[kv[i]] = kv[i+1]
	}
	return p
}

func TestCreditsFromMetadata(t *testing.T) {
	movie := map[string]any{
		"credits": map[string]any{
			"crew": []any{
				person("Writer A", "job", "Screenplay"),
				person("Director A", "job", "Director"),
				person("Director A", "job", "Director"),
				person("Director B", "job", "Director"),
				person("Director C", "job", "Director"),
			},
			"cast": []any{
				person("Actor A", "profile_path", "/aaaaaaaa.jpg"), person(" "), person("Actor B"), "junk",
				person("Actor C"), person("Actor D"), person("Actor E"), person("Actor F"),
				person("Actor G"), person("Actor H"), person("Actor I"),
			},
		},
	}
	series := map[string]any{
		"created_by": []any{person("Creator A")},
		"credits":    map[string]any{"cast": []any{person("Actor A")}},
	}
	adult := map[string]any{"adult": true, "credits": movie["credits"]}

	cases := []struct {
		name string
		md   map[string]any
		want *Credits
	}{
		{"movie: directors deduped and capped, cast capped in billing order, photo file name without the slash", movie, &Credits{
			Directors: []string{"Director A", "Director B"},
			Cast: []CastMember{{Name: "Actor A", Profile: "aaaaaaaa.jpg"}, {Name: "Actor B"}, {Name: "Actor C"}, {Name: "Actor D"},
				{Name: "Actor E"}, {Name: "Actor F"}, {Name: "Actor G"}, {Name: "Actor H"}},
		}},
		{"series without crew: creators only", series, &Credits{
			Creators: []string{"Creator A"},
			Cast:     []CastMember{{Name: "Actor A"}},
		}},
		{"adult title answers empty", adult, &Credits{}},
		{"row without credits answers empty", map[string]any{"title": "x"}, &Credits{}},
	}
	for _, c := range cases {
		if got := creditsFromMetadata(c.md); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestCreditsForTypeMismatch(t *testing.T) {
	film := &tm.Info{Type: tm.TmdbTypeMovie, Metadata: map[string]any{
		"credits": map[string]any{"cast": []any{person("Actor A")}},
	}}
	cases := []struct {
		name    string
		videoID string
		ct      models.ContentType
		want    *Credits
	}{
		{"tmdbN film row for a film", "tmdb7", models.ContentTypeMovie, &Credits{Cast: []CastMember{{Name: "Actor A"}}}},
		{"tmdbN film row for a series is another title", "tmdb7", models.ContentTypeSeries, &Credits{}},
		{"an IMDB id names one title, type hint ignored", "tt7", models.ContentTypeSeries, &Credits{Cast: []CastMember{{Name: "Actor A"}}}},
	}
	for _, c := range cases {
		if got := creditsFor(film, c.videoID, c.ct); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
