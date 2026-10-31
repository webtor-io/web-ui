package enrich

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
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
		if got := creditsFromMetadata(c.md, nil); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestOtherTitle(t *testing.T) {
	film := &tm.Info{Type: tm.TmdbTypeMovie}
	cases := []struct {
		name    string
		videoID string
		ct      models.ContentType
		want    bool
	}{
		{"tmdbN film row for a film", "tmdb7", models.ContentTypeMovie, false},
		{"tmdbN film row for a series is another title", "tmdb7", models.ContentTypeSeries, true},
		{"an IMDB id names one title, type hint ignored", "tt7", models.ContentTypeSeries, false},
	}
	for _, c := range cases {
		if got := otherTitle(film, c.videoID, c.ct); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// withID is person() with a TMDB person id, as JSON decodes it.
func withID(id float64, p map[string]any) map[string]any {
	p["id"] = id
	return p
}

func TestCreditsLocalizedNames(t *testing.T) {
	md := map[string]any{
		"created_by": []any{withID(30, person("Creator A"))},
		"credits": map[string]any{
			"crew": []any{
				withID(10, person("Director A", "job", "Director")),
				withID(11, person("Writer A", "job", "Screenplay")),
			},
			"cast": []any{
				withID(20, person("Actor A", "profile_path", "/aaaaaaaa.jpg")),
				withID(20, person("Actor A")), // a second role
				withID(21, person("Actor B")),
				withID(22, person("Actor B")), // another person of the same name
				person("Actor C"),
				person("Actor C"),
			},
		},
	}
	names := map[string]string{"10": "Режиссёр А", "30": "Создатель А", "20": "Актёр А", "22": "Актёр Б", "11": "Сценарист А"}
	want := &Credits{
		Directors: []string{"Режиссёр А"},
		Creators:  []string{"Создатель А"},
		// 21 is not in names and keeps the English name; id-less entries
		// are told apart by name.
		Cast: []CastMember{{Name: "Актёр А", Profile: "aaaaaaaa.jpg"}, {Name: "Actor B"}, {Name: "Актёр Б"}, {Name: "Actor C"}},
	}
	if got := creditsFromMetadata(md, names); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	english := &Credits{
		Directors: []string{"Director A"},
		Creators:  []string{"Creator A"},
		Cast:      []CastMember{{Name: "Actor A", Profile: "aaaaaaaa.jpg"}, {Name: "Actor B"}, {Name: "Actor B"}, {Name: "Actor C"}},
	}
	if got := creditsFromMetadata(md, nil); !reflect.DeepEqual(got, english) {
		t.Errorf("no names: got %+v, want %+v", got, english)
	}
}

func readDetails(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// The fixtures are TMDB's localized details responses (2026-10-31) cut down
// to the person fields.
func TestLocalizedNames(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		size    int
		has     map[string]string
		missing []string
	}{
		{"movie, ru", "localized_movie_ru.json", 21, map[string]string{
			"525":  "Кристофер Нолан", // the director
			"6193": "Леонардо ДиКаприо",
			"3899": "Кэн Ватанабэ",
			// No translation: TMDB's English name, kept like any other.
			"173212":  "Tohoru Masamune",
			"1677267": "Ryan Hayward", // cast entry 19 from 0, the last one read
		}, []string{
			"1334309", // cast entry 20
			"556",     // crew, not a director
		}},
		{"series, ru", "localized_series_ru.json", 9, map[string]string{
			"66633": "Винс Гиллиган", // created_by
			"17419": "Брайан Крэнстон",
		}, []string{"24951"}},
		{"movie, cs", "localized_movie_cs.json", 21, map[string]string{
			"525":  "Christopher Nolan",
			"3899": "Ken Watanabe", // English, where the original name is in Japanese script
		}, nil},
	}
	for _, c := range cases {
		got := localizedNames(readDetails(t, c.file))
		if len(got) != c.size {
			t.Errorf("%s: %d names, want %d: %v", c.name, len(got), c.size, got)
		}
		for id, want := range c.has {
			if got[id] != want {
				t.Errorf("%s: %s = %q, want %q", c.name, id, got[id], want)
			}
		}
		for _, id := range c.missing {
			if n, ok := got[id]; ok {
				t.Errorf("%s: %s = %q, want none", c.name, id, n)
			}
		}
	}

	raw := map[string]any{
		"created_by": []any{withID(1, person(" Создатель А "))},
		"credits": map[string]any{"cast": []any{
			withID(2, person("   ")), withID(3, person("—")), withID(4, person("1990")), withID(5, person("")),
			person("Без id"), "junk",
		}},
	}
	if got, want := localizedNames(raw), map[string]string{"1": "Создатель А"}; !reflect.DeepEqual(got, want) {
		t.Errorf("guards: got %v, want %v", got, want)
	}
	// The cast is read by position, as it is shown: a series' order has gaps.
	var cast []any
	for i, order := range []float64{0, 2, 3, 4, 5, 7, 22, 30} {
		p := withID(float64(100+i), person("Актёр "+strconv.Itoa(i)))
		p["order"] = order
		cast = append(cast, p)
	}
	got := localizedNames(map[string]any{"credits": map[string]any{"cast": cast}})
	if len(got) != 8 || got["106"] != "Актёр 6" || got["107"] != "Актёр 7" {
		t.Errorf("series cast with gaps in its order: got %v, want all 8 (orders 22 and 30 too)", got)
	}
	if got := localizedNames(map[string]any{}); got == nil || len(got) != 0 {
		t.Errorf("a response without credits: got %#v, want an empty map (stored as '{}', not NULL)", got)
	}
}
