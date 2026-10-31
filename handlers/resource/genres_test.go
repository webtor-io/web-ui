package resource

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/genre"
	"github.com/webtor-io/web-ui/services/i18n"
)

// The torrent page shows a title's genres in the header's meta row, to every
// visitor (docs/genres.md), and keeps them out of JSON-LD.

func genresMovie(genres []string) *models.Movie {
	year := int16(2001)
	m := creditsMovie("tt1")
	m.MovieMetadata.VideoMetadata.Year = &year
	m.MovieMetadata.VideoMetadata.Genres = genres
	return m
}

func TestGetEnrichedGenres(t *testing.T) {
	h := NewHelper("")
	series := &models.Series{SeriesMetadata: &models.SeriesMetadata{VideoMetadata: &models.VideoMetadata{VideoID: "tt2", Genres: []string{"drama", "war", "politics"}}}}
	cases := []struct {
		name string
		d    *GetData
		want []string
	}{
		{"no title", &GetData{}, nil},
		{"not enriched", &GetData{Movie: &models.Movie{}}, nil},
		{"not derived (NULL)", &GetData{Movie: genresMovie(nil)}, nil},
		{"derived, none ('{}')", &GetData{Movie: genresMovie([]string{})}, nil},
		{"movie", &GetData{Movie: genresMovie([]string{"scifi", "action"})}, []string{"scifi", "action"}},
		{"series", &GetData{Series: series}, []string{"drama", "war", "politics"}},
		// A value outside the vocabulary would print as the raw key "genre.x".
		{"unknown value dropped", &GetData{Movie: genresMovie([]string{"Action", "drama", "sci-fi"})}, []string{"drama"}},
	}
	for _, c := range cases {
		if got := h.GetEnrichedGenres(c.d); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

var metaRowRe = regexp.MustCompile(`(?s)<div class="flex flex-wrap items-center gap-2 mb-1 text-sm text-w-sub">(.*?)</div>`)

// metaRow is the header's year · rating · Series · genres row, whitespace
// collapsed.
func metaRow(t *testing.T, html string) string {
	t.Helper()
	m := metaRowRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("meta row not rendered")
	}
	return strings.Join(strings.Fields(m[1]), " ")
}

func TestGenreLine(t *testing.T) {
	tpl := resourceTemplates(t)
	render := func(lang string, genres []string, adult bool) string {
		gd := intentPage("", false)
		gd.Movie = genresMovie(genres)
		if adult {
			gd.ResourceMetadata = &models.ResourceMetadata{IsAdult: true}
		}
		// pageContext's visitor is anonymous: the line is not behind sign-in.
		return metaRow(t, execute(t, tpl, "main", pageContext(lang, gd)))
	}
	five := []string{"action", "adventure", "scifi", "fantasy", "drama"}

	if got, want := render("en", five, false), `<span>2001</span> <span>Action, Adventure, Sci-Fi, Fantasy</span>`; got != want {
		t.Errorf("en: got %q, want %q (four labels, the fifth dropped)", got, want)
	}
	// RU keeps "Мультфильм" for series too, as Russian catalogues do
	// (docs/i18n.md): not a stray film noun to translate away.
	if got, want := render("ru", []string{"animation", "family"}, false), `<span>2001</span> <span>Мультфильм, Семейный</span>`; got != want {
		t.Errorf("ru: got %q, want %q", got, want)
	}
	if got := render("en", []string{"horror"}, true); !strings.Contains(got, `<span>Horror</span>`) {
		t.Errorf("adult torrent: genres hidden, row %q", got)
	}
	for name, g := range map[string][]string{"nil": nil, "empty": {}} {
		if got := render("en", g, false); got != `<span>2001</span>` {
			t.Errorf("%s genres: row %q, want the year alone", name, got)
		}
	}

	// On a phone the genres wrap onto lines of their own: the Series badge
	// goes before them, or it is left alone on the line below.
	gd := intentPage("", false)
	year, rating := int16(2019), 8.1
	gd.Series = &models.Series{SeriesMetadata: &models.SeriesMetadata{VideoMetadata: &models.VideoMetadata{
		VideoID: "tt2", Title: "Series A", Year: &year, Rating: &rating, Genres: []string{"action", "adventure", "scifi", "fantasy"},
	}}}
	row := metaRow(t, execute(t, tpl, "main", pageContext("en", gd)))
	order := regexp.MustCompile(`^<span>2019</span> <span class="flex items-center gap-1 text-yellow-400"> <svg .*?</svg> 8\.1 </span> <span class="badge [^"]*">Series</span> <span>Action, Adventure, Sci-Fi, Fantasy</span>$`)
	if !order.MatchString(row) {
		t.Errorf("series: row %q, want year, rating, Series badge, genres", row)
	}
}

// TestGenreLabelsFitACardLine: library and Discover cards show genres on one
// line-clamp-1 line, about 134px at 12px on a 360px phone. A rune count
// stands in for the width: es "Programa de entrevistas" (23) measured 138px
// and was cut to "Programa de entrevis…"; the longest label left is 17.
func TestGenreLabelsFitACardLine(t *testing.T) {
	root, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer func() { _ = root.Close() }()
	h := i18n.NewHelper(i18n.New(root.FS()))
	for _, lang := range i18n.SupportedLangs {
		for _, slug := range genre.Slugs {
			if l := h.T(lang, "genre."+slug); utf8.RuneCountInString(l) > 20 {
				t.Errorf("%s genre.%s %q: %d characters, over 20", lang, slug, l, utf8.RuneCountInString(l))
			}
		}
	}
}

func TestGenresNotInJSONLD(t *testing.T) {
	tpl := resourceTemplates(t)
	gd := intentPage("", false)
	gd.Movie = genresMovie([]string{"drama", "war"})
	out := execute(t, tpl, "head_extra", pageContext("en", gd))
	_, body, ok := strings.Cut(out, `<script type="application/ld+json">`)
	if !ok {
		t.Fatalf("no JSON-LD in %q", out)
	}
	body, _, _ = strings.Cut(body, "</script>")
	var ld map[string]any
	if err := json.Unmarshal([]byte(body), &ld); err != nil {
		t.Fatalf("JSON-LD does not parse: %v\n%s", err, body)
	}
	if ld["name"] != "Movie A" {
		t.Errorf("not the title's JSON-LD: %v", ld)
	}
	if _, has := ld["genre"]; has || strings.Contains(strings.ToLower(body), "genre") {
		t.Errorf("JSON-LD carries genres: %s", body)
	}
}
