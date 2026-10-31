package library

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/library/helpers"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

func TestBindIndexArgsGenre(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for raw, want := range map[string]string{
		"drama":           "drama",
		"film_noir":       "film_noir",
		"":                "",
		"Drama":           "", // addon spelling, not a slug
		"genre.drama":     "",
		"drama' OR '1'=1": "",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/lib/movies?genre="+strings.ReplaceAll(raw, " ", "+"), nil)
		c.Params = gin.Params{{Key: "type", Value: "movies"}}
		if got := (&Handler{}).bindIndexArgs(c).Genre; got != want {
			t.Errorf("genre=%q: got %q, want %q", raw, got, want)
		}
	}
}

func TestMakeGenreOptions(t *testing.T) {
	labels := map[string]string{"drama": "Dram", "kids": "Çocuk", "war": "Savaş"}
	label := func(slug string) string { return labels[slug] }
	var got []string
	for _, o := range makeGenreOptions([]string{"war", "drama", "kids", "not_a_slug"}, "", "tr", label) {
		got = append(got, o.Label)
	}
	// Turkish order, not code-point order: Ç sorts after C, before D.
	if want := []string{"Çocuk", "Dram", "Savaş"}; !reflect.DeepEqual(got, want) {
		t.Errorf("labels: got %v, want %v", got, want)
	}

	// The selected genre stays an option when nothing in the section has it
	// any more, so the select still shows what the list is filtered by.
	opts := makeGenreOptions([]string{"drama"}, "war", "tr", label)
	if len(opts) != 2 || opts[1].Slug != "war" || !opts[1].Selected || opts[0].Selected {
		t.Errorf("selected genre: got %+v", opts)
	}
	if opts := makeGenreOptions(nil, "", "en", label); len(opts) != 0 {
		t.Errorf("no genres must give no options, got %+v", opts)
	}
}

func TestSectionEmpty(t *testing.T) {
	for name, c := range map[string]struct {
		d    IndexData
		want bool
	}{
		"filtered to nothing": {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies}, MovieCount: 3}, false},
		"really empty":        {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies}, TorrentCount: 3}, true},
		"items":               {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeSeries}, Items: []any{1}}, false},
		"empty torrents":      {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeTorrents}, MovieCount: 3}, true},
		"unknown section":     {IndexData{Args: &shared.IndexArgs{Section: "foo"}, MovieCount: 3}, true},
	} {
		if got := c.d.SectionEmpty(); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

// funcsOf registers a helper's methods the way template.Manager.WithHelper
// does: the method name with a lowercase first letter.
func funcsOf(fm template.FuncMap, h any) {
	v := reflect.ValueOf(h)
	for i := 0; i < v.NumMethod(); i++ {
		name := v.Type().Method(i).Name
		r, size := utf8.DecodeRuneInString(name)
		fm[string(unicode.ToLower(r))+name[size:]] = v.Method(i).Interface()
	}
}

func parseLibrary(t *testing.T) *template.Template {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	fm := template.FuncMap{"langPath": i18n.LangPath}
	funcsOf(fm, &web.Helper{})
	fm["asset"] = func(name string) template.HTML { return template.HTML("<!--" + name + "-->") }
	funcsOf(fm, i18n.NewHelper(i18n.New(locales.FS())))
	funcsOf(fm, helpers.NewVideoContentHelper())
	funcsOf(fm, helpers.NewSortHelper())
	funcsOf(fm, helpers.NewMenuHelper())
	funcsOf(fm, helpers.NewStarsHelper())
	tpl, err := template.New("index.html").Funcs(fm).ParseFiles(
		"../../templates/views/library/index.html",
		"../../templates/partials/bulk_select.html",
	)
	if err != nil {
		t.Fatal(err)
	}
	if tpl, err = tpl.ParseGlob("../../templates/partials/library/*.html"); err != nil {
		t.Fatal(err)
	}
	return tpl
}

func render(t *testing.T, tpl *template.Template, body string, ctx *web.Context) string {
	t.Helper()
	tpl, err := template.Must(tpl.Clone()).Parse(`{{ define "body" }}` + body + `{{ end }}`)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, "body", ctx); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func movie(title string, genres ...string) *models.Movie {
	return &models.Movie{
		VideoContent:  &models.VideoContent{ResourceID: "r-" + title, Title: title},
		MovieMetadata: &models.MovieMetadata{VideoMetadata: &models.VideoMetadata{VideoID: "tt-" + title, Title: title, Genres: genres}},
	}
}

var formRe = regexp.MustCompile(`(?s)<form ([^>]*)>(.*?)</form>`)

// filterForms finds the library's filter form by what it holds, the only
// form with selects: [whole, attributes, body] each.
func filterForms(out string) (forms [][]string) {
	for _, f := range formRe.FindAllStringSubmatch(out, -1) {
		if strings.Contains(f[2], "<select") {
			forms = append(forms, f)
		}
	}
	return
}

func TestLibraryFilterFormAndCards(t *testing.T) {
	tpl := parseLibrary(t)
	args := &shared.IndexArgs{Section: shared.SectionTypeMovies, Sort: models.SortTypeYear, Watched: shared.WatchedFilterUnwatched, Genre: "drama"}
	data := &IndexData{
		Args:       args,
		Items:      []any{movie("A", "drama", "war", "comedy", "crime"), movie("B", "bogus", "western"), movie("C")},
		MovieCount: 3,
		Genres:     []GenreOption{{Slug: "drama", Label: "Drama", Selected: true}, {Slug: "war", Label: "War"}},
	}
	out := render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/movies", Data: data})

	// One form carries sort, watched and genre, so a change of any select
	// keeps the other two.
	forms := filterForms(out)
	if len(forms) != 1 {
		t.Fatalf("want one filter form, got %d", len(forms))
	}
	for _, name := range []string{"watched", "genre", "sort"} {
		if !regexp.MustCompile(`<select [^>]*name="` + name + `"`).MatchString(forms[0][2]) {
			t.Errorf("the filter form has no %s select", name)
		}
	}
	for _, want := range []string{
		`<option value="drama" selected>Drama</option>`,
		`<option value="unwatched" selected>`,
		`<option class="capitalize" value="2" selected>`,
		`aria-label="Genre"`,
		`>All genres</option>`,
	} {
		if !strings.Contains(forms[0][2], want) {
			t.Errorf("filter form lacks %s", want)
		}
	}

	// The card's genre line: three labels at most, slugs outside the
	// vocabulary dropped, nothing at all for a title without genres.
	lines := regexp.MustCompile(`<p class="mt-1 text-xs text-w-muted line-clamp-1">([^<]*)</p>`).FindAllStringSubmatch(out, -1)
	var got []string
	for _, l := range lines {
		got = append(got, l[1])
	}
	if want := []string{"Drama, War, Comedy", "Western"}; !reflect.DeepEqual(got, want) {
		t.Errorf("genre lines: got %q, want %q", got, want)
	}

	// The rating badge renders the page again: it has to ask for the same
	// sort and filters, as numbers and slugs.
	rate := regexp.MustCompile(`(?s)<form method="get"[^>]*>(.*?)</form>`).FindAllStringSubmatch(out, -1)
	if len(rate) != 3 {
		t.Fatalf("want a rating form per card, got %d", len(rate))
	}
	for _, want := range []string{`name="sort" value="2"`, `name="watched" value="unwatched"`, `name="genre" value="drama"`} {
		if !strings.Contains(rate[0][1], want) {
			t.Errorf("rating form lacks %s:\n%s", want, rate[0][1])
		}
	}
}

func TestLibraryGenreSelectHiddenWithoutGenres(t *testing.T) {
	tpl := parseLibrary(t)
	data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies}, Items: []any{movie("A")}, MovieCount: 1}
	out := render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/movies", Data: data})
	forms := filterForms(out)
	if len(forms) != 1 {
		t.Fatal("no filter form")
	}
	if strings.Contains(forms[0][2], `name="genre"`) {
		t.Error("a section without genres must not show the genre select")
	}
	if !strings.Contains(forms[0][2], `name="watched"`) {
		t.Error("the watched select is gone")
	}
}

// The filter form renders main again -- main's own layout, the one its
// history entry replays -- and leaves that entry: Back then restores the
// sticky bar's selects together with the list. Aimed at #list it swapped only
// the list, the selects kept the newer values and sent them with the next
// change, and its entry replayed on a resource page blanked that page's #list.
func TestLibraryFilterFormSwapsMain(t *testing.T) {
	tpl := parseLibrary(t)
	data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies}, Items: []any{movie("A")}, MovieCount: 1}
	out := render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/movies", Data: data})
	forms := filterForms(out)
	if len(forms) != 1 {
		t.Fatalf("want one filter form, got %d", len(forms))
	}
	for _, want := range []string{`action="/lib/movies"`, `data-async-target="main"`, `data-async-no-scroll="true"`} {
		if !strings.Contains(forms[0][1], want) {
			t.Errorf("filter form: no %s in <form %s>", want, forms[0][1])
		}
	}
	if strings.Contains(forms[0][1], `data-async-push-state="false"`) {
		t.Error("filter form: a change must leave a history entry")
	}
}

// Filters that match nothing say so, with the filter form still there to
// change them and a reset that keeps the sort, which is not a filter; only a
// really empty section gets "No torrents yet".
func TestLibraryFilterEmpty(t *testing.T) {
	tpl := parseLibrary(t)
	page := func(lang string, count int) string {
		data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies, Sort: models.SortTypeYear, Genre: "war"}, MovieCount: count}
		return render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: lang, Path: "/lib/movies", Data: data})
	}
	filtered := page("en", 3)
	for _, want := range []string{"Nothing in this section matches the filters.", `href="/lib/movies?sort=2" data-async-target="main">Reset filters</a>`} {
		if !strings.Contains(filtered, want) {
			t.Errorf("filtered to nothing: no %s", want)
		}
	}
	if strings.Contains(filtered, "No torrents yet") {
		t.Error("filtered to nothing: shows the empty-section state")
	}
	if len(filterForms(filtered)) != 1 {
		t.Error("filtered to nothing: the filter form is gone")
	}
	if ru := page("ru", 3); !strings.Contains(ru, "В этом разделе ничего не подходит под фильтры.") || !strings.Contains(ru, `href="/ru/lib/movies?sort=2"`) {
		t.Error("filtered to nothing, ru: no message, or no reset that keeps the sort")
	}

	empty := page("en", 0)
	if !strings.Contains(empty, "No torrents yet") || strings.Contains(empty, `id="list"`) || len(filterForms(empty)) != 0 {
		t.Error("empty section: want the empty state and neither #list nor the filter form")
	}
}

// "Select" is offered only with something to select. The bar is rendered
// with main, the filter form's target, so it comes back with the titles.
func TestLibraryBulkSelectNeedsItems(t *testing.T) {
	tpl := parseLibrary(t)
	page := func(items ...any) string {
		data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeMovies, Genre: "war"}, Items: items, MovieCount: 3}
		return render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/movies", Data: data})
	}
	bulk := []string{"js-bulk-toggle", `id="bulk-remove"`, "<!--bulk_select.js-->"}
	with := page(movie("A"))
	for _, b := range bulk {
		if !strings.Contains(with, b) {
			t.Errorf("titles in the list: no %s", b)
		}
	}
	without := page()
	for _, b := range bulk {
		if strings.Contains(without, b) {
			t.Errorf("filtered to nothing: offers select mode (%s)", b)
		}
	}
}
