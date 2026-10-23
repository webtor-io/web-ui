package resource

import (
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
)

func creditsMovie(videoID string) *models.Movie {
	return &models.Movie{MovieMetadata: &models.MovieMetadata{VideoMetadata: &models.VideoMetadata{VideoID: videoID, Title: "Movie A"}}}
}

func TestCreditsTarget(t *testing.T) {
	series := &models.Series{SeriesMetadata: &models.SeriesMetadata{VideoMetadata: &models.VideoMetadata{VideoID: "tt2"}}}
	cases := []struct {
		name   string
		d      *GetData
		u      *auth.User
		wantID string
		wantCT models.ContentType
		wantOK bool
	}{
		{"signed-in movie", &GetData{Movie: creditsMovie("tt1")}, signedIn, "tt1", models.ContentTypeMovie, true},
		{"signed-in series", &GetData{Series: series}, signedIn, "tt2", models.ContentTypeSeries, true},
		{"anonymous", &GetData{Movie: creditsMovie("tt1")}, &auth.User{}, "", models.ContentTypeMovie, false},
		{"adult torrent", &GetData{Movie: creditsMovie("tt1"), ResourceMetadata: &models.ResourceMetadata{IsAdult: true}}, signedIn, "", models.ContentTypeMovie, false},
		{"unclassified torrent", &GetData{Movie: creditsMovie("tt1"), ResourceMetadata: nil}, signedIn, "tt1", models.ContentTypeMovie, true},
		{"classification read failed", &GetData{Movie: creditsMovie("tt1"), adultUnknown: true}, signedIn, "", models.ContentTypeMovie, false},
		{"not enriched", &GetData{Movie: &models.Movie{}}, signedIn, "", models.ContentTypeMovie, false},
		{"no title", &GetData{}, signedIn, "", models.ContentTypeMovie, false},
	}
	for _, c := range cases {
		id, ct, ok := creditsTarget(c.d, c.u)
		if id != c.wantID || ct != c.wantCT || ok != c.wantOK {
			t.Errorf("%s: got (%q, %v, %v), want (%q, %v, %v)", c.name, id, ct, ok, c.wantID, c.wantCT, c.wantOK)
		}
	}
}

func TestCreditsBlock(t *testing.T) {
	tpl := resourceTemplates(t)
	render := func(lang string, cr *enrich.Credits) string {
		gd := intentPage("", false)
		gd.Movie = creditsMovie("tt1")
		gd.Credits = cr
		return execute(t, tpl, "main", pageContext(lang, gd))
	}

	got := render("en", &enrich.Credits{Directors: []string{"Director A", "Director B"}, Cast: []enrich.CastMember{
		{Name: "Actor A", Profile: "aaaaaaaa.jpg"}, {Name: "Actor B"}, {Name: "Actor C"}, {Name: "Actor D"}, {Name: "Actor E"},
	}})
	for _, want := range []string{
		`<span class="text-w-muted">Director:</span> Director A, Director B</p>`,
		// The collapsed line names the first four; the fifth is in the strip only.
		`<span class="text-w-muted">Cast:</span> <span class="group-open:hidden">Actor A, Actor B, Actor C, Actor D</span>`,
		`<img src="/lib/person/photo/aaaaaaaa.jpg/96.jpg"`,
		`line-clamp-2">Actor E</div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "Creator:") {
		t.Error("an empty creators list rendered a line")
	}
	if n := strings.Count(got, "<img src=\"/lib/person/photo/"); n != 1 {
		t.Errorf("%d photos, want 1: the others have no profile and show the placeholder", n)
	}

	if got := render("ru", &enrich.Credits{Creators: []string{"Creator A"}}); !strings.Contains(got, `Создатель:</span> Creator A`) {
		t.Error("ru creators line missing")
	}

	for name, cr := range map[string]*enrich.Credits{"nil": nil, "empty": {}} {
		if got := render("en", cr); strings.Contains(got, "credits.") || strings.Contains(got, "<details") || strings.Contains(got, "space-y-1") {
			t.Errorf("%s credits rendered the block", name)
		}
	}
}
