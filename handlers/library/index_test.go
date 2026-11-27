package library

import (
	"bytes"
	"context"
	"flag"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/handlers/library/helpers"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/uploads"
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
		"empty uploads":       {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeUploads}, UploadCount: 0}, true},
		"uploads counted":     {IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeUploads}, UploadCount: 2}, false},
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

const (
	testTorrent = "0123456789abcdef0123456789abcdef01234567"
	testUpload  = "9c5db5752b7c0123456789abcdef0123456789ab"
)

// withUploads makes the parsed library answer uploadsEnabled with on.
func withUploads(tpl *template.Template, on bool) *template.Template {
	return tpl.Funcs(template.FuncMap{"uploadsEnabled": func() bool { return on }})
}

// My files: its select bar deletes uploads, and asks about deleting them;
// a row says when the upload last changed, not when it was added.
func TestUploadsSectionRenders(t *testing.T) {
	now := time.Now()
	updated := now.Add(-2 * time.Hour)
	added := now.Add(-5 * 24 * time.Hour)
	row := &models.Library{ResourceID: testUpload, CreatedAt: added, Torrent: &models.TorrentResource{Name: "Photos", SizeBytes: 1 << 20, FileCount: 2, UpdatedAt: &updated}}
	page := func(t *testing.T, section shared.SectionType, on bool, items ...any) string {
		tpl := withUploads(parseLibrary(t), on)
		data := &IndexData{Args: &shared.IndexArgs{Section: section}, Items: items, TorrentCount: 1, UploadCount: len(items)}
		return render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/" + string(section), Data: data})
	}
	h := &web.Helper{}

	t.Run("uploads", func(t *testing.T) {
		out := page(t, shared.SectionTypeUploads, true, row)
		for _, want := range []string{
			`<form id="bulk-remove"`,
			`action="/upload/delete"`,
			`data-bulk-confirm="Delete the selection with all its files for good? Vault copies will be deleted too."`,
			`href="/` + testUpload + `"`,
			h.TimeAgoLang("en", updated),
			`class="flex flex-wrap gap-1.5 sm:gap-2"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("no %s", want)
			}
		}
		if strings.Contains(out, h.TimeAgoLang("en", added)) {
			t.Errorf("the row says when the upload was added (%s), not when it changed", h.TimeAgoLang("en", added))
		}
		if !regexp.MustCompile(`href="/lib/uploads"[^>]*>\s*My files\s*<span[^>]*>1</span>`).MatchString(out) {
			t.Error("the menu has no My files item counting 1")
		}
		// The one-word tabs are capitalised by CSS; "My files" is a phrase in
		// sentence case, which capitalize would turn into "My Files" ("Мои
		// Файлы", "I Miei File").
		class := func(section string) string {
			m := regexp.MustCompile(`<a class="([^"]*)" href="/lib/` + section + `"`).FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("no %s tab", section)
			}
			return m[1]
		}
		if strings.Contains(class("uploads"), "capitalize") || !strings.Contains(class("movies"), "capitalize") {
			t.Errorf("tabs: uploads %q, movies %q; want only the one-word tab capitalised", class("uploads"), class("movies"))
		}
	})

	// The bulk action of My files deletes for good, and says so; the
	// Torrents one takes rows out of the library.
	bulkLabel := regexp.MustCompile(`(?s)class="js-bulk-submit[^>]*>.*?</svg>\s*(\S[^<]*)<span class="js-bulk-count">`)
	label := func(t *testing.T, out string) string {
		t.Helper()
		m := bulkLabel.FindStringSubmatch(out)
		if m == nil {
			t.Fatal("no bulk submit")
		}
		return m[1]
	}
	if got := label(t, page(t, shared.SectionTypeUploads, true, row)); got != "Delete" {
		t.Errorf("My files: the bulk action is %q, want Delete", got)
	}
	if got := label(t, page(t, shared.SectionTypeTorrents, true, row)); got != "Remove" {
		t.Errorf("Torrents: the bulk action is %q, want Remove", got)
	}

	// My files says how much of the space is used, by the sort; nothing
	// without a count (uploads off, no claims, a failed count).
	t.Run("quota", func(t *testing.T) {
		data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeUploads}, Items: []any{row}, UploadCount: 1, Quota: &uploads.Usage{Used: 3 << 30, Cap: 50 << 30}}
		tpl := withUploads(parseLibrary(t), true)
		for lang, want := range map[string]string{"en": "3.0\u00a0GB of 50\u00a0GB used", "ru": "Занято 3.0\u00a0GB из 50\u00a0GB"} {
			out := render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: lang, Path: "/lib/uploads", Data: data})
			if !strings.Contains(out, want) {
				t.Errorf("%s: no %q", lang, want)
			}
			if i, j := strings.Index(out, want), strings.Index(out, `name="sort"`); i < 0 || j < 0 || i > j {
				t.Errorf("%s: the quota at %d, the sort at %d; want it just before", lang, i, j)
			}
		}
		data.Quota = nil
		if out := render(t, tpl, `{{ template "main" . }}`, &web.Context{Lang: "en", Path: "/lib/uploads", Data: data}); strings.Contains(out, "50\u00a0GB") {
			t.Error("a quota line without a count")
		}
	})

	t.Run("torrents", func(t *testing.T) {
		out := page(t, shared.SectionTypeTorrents, true, &models.Library{ResourceID: testTorrent, CreatedAt: added, Torrent: &models.TorrentResource{Name: "Film"}})
		for _, want := range []string{
			`action="/lib/remove"`,
			`data-bulk-confirm="Remove the selected items from your library? The torrents themselves stay available."`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("no %s", want)
			}
		}
		if strings.Contains(out, `action="/upload/delete"`) {
			t.Error("the torrents bar deletes uploads")
		}
	})

	// Uploads turned off: the rows stay visible and open, but nothing
	// deletes them here -- /upload/delete is not served then, and
	// /lib/remove would leave their files with no row.
	t.Run("uploads off", func(t *testing.T) {
		out := page(t, shared.SectionTypeUploads, false, row)
		for _, bad := range []string{`id="bulk-remove"`, `action="/upload/delete"`} {
			if strings.Contains(out, bad) {
				t.Errorf("has %s", bad)
			}
		}
		if !strings.Contains(out, `href="/`+testUpload+`"`) {
			t.Error("the upload's row is gone")
		}
	})

	// Files storage removed: the row is dimmed, says so where the size was
	// and leads nowhere -- its page would only say they are gone. Select
	// mode still deletes it.
	t.Run("gone", func(t *testing.T) {
		gone := *row
		gone.Gone = true
		out := page(t, shared.SectionTypeUploads, true, &gone)
		for _, want := range []string{`opacity-60`, `Removed from storage · 2 files`, `class="js-bulk-box checkbox checkbox-sm" value="` + testUpload + `"`} {
			if !strings.Contains(out, want) {
				t.Errorf("no %s", want)
			}
		}
		for _, bad := range []string{`href="/` + testUpload + `"`, h.BitsForHumans(1 << 20)} {
			if strings.Contains(out, bad) {
				t.Errorf("has %s", bad)
			}
		}
		if live := page(t, shared.SectionTypeUploads, true, row); strings.Contains(live, `opacity-60`) || strings.Contains(live, `Removed from storage`) {
			t.Error("a live row is dimmed")
		}
	})

	t.Run("empty", func(t *testing.T) {
		out := page(t, shared.SectionTypeUploads, true)
		for _, want := range []string{"Nothing here yet", `href="/" data-async-target="main">Upload files</a>`} {
			if !strings.Contains(out, want) {
				t.Errorf("no %s", want)
			}
		}
		if strings.Contains(out, "No torrents yet") {
			t.Error("says there are no torrents")
		}
		if off := page(t, shared.SectionTypeUploads, false); !strings.Contains(off, "Nothing here yet") || strings.Contains(off, "Upload files") {
			t.Error("uploads off: want the empty state without the upload button")
		}
	})
}

// My files is in the menu while uploads are on, and while there are any
// to see after they were turned off.
func TestUploadsMenuItem(t *testing.T) {
	for _, c := range []struct {
		on    bool
		count int
		want  bool
	}{
		{false, 0, false},
		{false, 2, true},
		{true, 0, true},
	} {
		tpl := withUploads(parseLibrary(t), c.on)
		data := &IndexData{Args: &shared.IndexArgs{Section: shared.SectionTypeTorrents}, TorrentCount: 1, UploadCount: c.count}
		out := render(t, tpl, `{{ template "library/menu" . }}`, &web.Context{Lang: "en", Path: "/lib/", Data: data})
		if got := strings.Contains(out, `href="/lib/uploads"`); got != c.want {
			t.Errorf("uploads on %v, %d uploads: item shown %v, want %v", c.on, c.count, got, c.want)
		}
		if c.want && !regexp.MustCompile(`href="/lib/uploads"[^>]*>[^<]*<span[^>]*>`+strconv.Itoa(c.count)+`</span>`).MatchString(out) {
			t.Errorf("uploads on %v, %d uploads: the item does not count %d", c.on, c.count, c.count)
		}
	}
}

// My files marks the uploads whose files storage removed: those whose
// reserve has gone_at. Another account's mark and an upload without a
// reserve row leave a row as it is.
func TestUploadsGoneRows(t *testing.T) {
	const (
		gone = "9c5db5752b7c0a23456789abcdef0123456789ab"
		live = "9c5db5752b7c0b23456789abcdef0123456789ab"
		bare = "9c5db5752b7c0c23456789abcdef0123456789ab"
	)
	db := pgtest.Start(t)
	h := &Handler{pg: pgtest.CS(t, db)}
	user := func() uuid.UUID {
		var id uuid.UUID
		if _, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	u, other := user(), user()
	for _, id := range []string{gone, live, bare} {
		if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, ?, 1, 1)`, id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO library (user_id, resource_id, name) VALUES (?, ?, 'n')`, u, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct {
		user uuid.UUID
		id   string
		gone bool
	}{{u, gone, true}, {u, live, false}, {other, live, true}} {
		if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, gone_at) VALUES (?, ?, 1, CASE WHEN ? THEN now() END)`, r.user, r.id, r.gone); err != nil {
			t.Fatal(err)
		}
	}
	items, err := h.getLibraryList(context.Background(), &auth.User{ID: u}, &shared.IndexArgs{Section: shared.SectionTypeUploads, Sort: models.SortTypeName})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		l := it.(*models.Library)
		got[l.ResourceID] = l.Gone
	}
	if want := map[string]bool{gone: true, live: false, bare: false}; !reflect.DeepEqual(got, want) {
		t.Errorf("gone: %v, want %v", got, want)
	}
}

// The space used is counted for My files alone: the other sections show no
// line and run no count.
func TestQuotaOnlyInMyFiles(t *testing.T) {
	db := pgtest.Start(t)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range uploads.RegisterFlags(api.RegisterFlags(nil)) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--upload-url=http://127.0.0.1:1", "--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	up := uploads.New(c, pgtest.CS(t, db), api.New(c, http.DefaultClient))
	if up == nil {
		t.Fatal("uploads are off")
	}
	h := &Handler{uploads: up}
	var id uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: id}
	free := &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0}}}
	ctx := context.Background()
	if got := h.quota(ctx, u, free, shared.SectionTypeUploads); got == nil || got.Cap != uploads.FreeCap {
		t.Errorf("My files: %+v, want the free cap", got)
	}
	for _, sec := range []shared.SectionType{shared.SectionTypeTorrents, shared.SectionTypeMovies, shared.SectionTypeSeries} {
		if got := h.quota(ctx, u, free, sec); got != nil {
			t.Errorf("%s: %+v, want no line", sec, got)
		}
	}
}
