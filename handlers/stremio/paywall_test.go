package stremio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	co "github.com/webtor-io/web-ui/services/link_resolver/common"
	"github.com/webtor-io/web-ui/services/metrics/metricstest"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/web"
)

const testSecret = "test-secret"

// fakeResolver answers ResolveLink with a fixed result, and the file
// pickers are never reached: the tokens below carry idx.
type fakeResolver struct {
	res   *co.LinkResult
	err   error
	calls int
}

func (f *fakeResolver) ResolveLink(context.Context, uuid.UUID, *api.Claims, *claims.Data, string, int, bool) (*co.LinkResult, error) {
	f.calls++
	return f.res, f.err
}

func (f *fakeResolver) PickEpisodeFileIdx(context.Context, *api.Claims, string, int, int) (int, error) {
	return 0, errors.New("not expected")
}

func (f *fakeResolver) PickPrimaryFileIdx(context.Context, *api.Claims, string) (int, error) {
	return 0, errors.New("not expected")
}

type fakePromo struct{ o *offer.Offer }

func (f fakePromo) Promo() *offer.Offer { return f.o }

// trialPlan is the production shape: a plan whose checkout starts a trial.
var trialPlan = &offer.Offer{Tier: "silver", PeriodDays: 30, TrialDays: 7, URL: "https://checkout.example/silver?trial"}

// clipsIn writes stand-in clips for langs into a temporary pub/stremio.
func clipsIn(t *testing.T, langs ...string) *paywallClips {
	t.Helper()
	dir := t.TempDir()
	for _, l := range langs {
		if err := os.WriteFile(filepath.Join(dir, "paywall-"+l+".mp4"), []byte("clip "+l), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Not a clip: must be ignored rather than read as language "xx".
	if err := os.WriteFile(filepath.Join(dir, "paywall-xx.webm"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := loadPaywallClips(dir, "https://webtor.io")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func playbackToken(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"hash": "08ada5a7a6183aae1e09d831df6748d566095a10",
		"idx":  3,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type resolveCase struct {
	res         *fakeResolver
	promo       *offer.Offer
	clips       *paywallClips
	accountLang *string
	method      string
}

// resolveWith mounts resolve the way RegisterHandler does, behind a
// signed-in addon user whose settings carry accountLang.
func resolveWith(t *testing.T, rc resolveCase) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &Handler{secret: testSecret, res: rc.res, offers: fakePromo{rc.promo}, clips: rc.clips}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		web.SetUserSettings(c, &models.UserSettings{Lang: rc.accountLang})
		c.Next()
	})
	r.Match([]string{http.MethodGet, http.MethodHead}, "/stremio/resolve/*data", h.resolve)
	method := rc.method
	if method == "" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, "/stremio/resolve/"+playbackToken(t), nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContext{},
		&models.User{UserID: uuid.FromStringOrNil("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), Email: "viewer@example.com"}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func lang(s string) *string { return &s }

func paywalled() *fakeResolver { return &fakeResolver{err: lr.ErrPlanRequired} }

func TestPaywallRedirectsToTheClipInTheAccountLanguage(t *testing.T) {
	clips := clipsIn(t, "en", "ru")
	before := metricstest.Counter(t, "webui_stremio_paywall_video_total", map[string]string{"lang": "ru", "method": "GET"})

	w := resolveWith(t, resolveCase{res: paywalled(), promo: trialPlan, clips: clips, accountLang: lang("ru")})

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the clip", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://webtor.io/pub/stremio/paywall-ru.mp4?v=") {
		t.Errorf("Location = %q, want the absolute URL of the Russian clip", loc)
	}
	after := metricstest.Counter(t, "webui_stremio_paywall_video_total", map[string]string{"lang": "ru", "method": "GET"})
	if after != before+1 {
		t.Errorf("paywall counter moved by %v, want 1", after-before)
	}
}

// Accounts that never browsed a prefixed page have no language stored, and
// a language nobody rendered a clip for must not 404: both get English.
func TestPaywallFallsBackToEnglish(t *testing.T) {
	clips := clipsIn(t, "en", "ru")
	for name, l := range map[string]*string{"no language stored": nil, "empty": lang(""), "no clip for it": lang("de"), "unknown": lang("zz")} {
		t.Run(name, func(t *testing.T) {
			w := resolveWith(t, resolveCase{res: paywalled(), promo: trialPlan, clips: clips, accountLang: l})
			if loc := w.Header().Get("Location"); w.Code != http.StatusFound || !strings.HasPrefix(loc, "https://webtor.io/pub/stremio/paywall-en.mp4?v=") {
				t.Errorf("%d %q, want 302 to the English clip", w.Code, loc)
			}
		})
	}
}

// Stremio probes the next episode's URL with HEAD before binge-playing it;
// the probe must see the same redirect, or binge drops to source selection.
func TestPaywallAnswersTheHEADProbe(t *testing.T) {
	w := resolveWith(t, resolveCase{res: paywalled(), promo: trialPlan, clips: clipsIn(t, "en"), method: http.MethodHead})
	if loc := w.Header().Get("Location"); w.Code != http.StatusFound || !strings.HasPrefix(loc, "https://webtor.io/pub/stremio/paywall-en.mp4") {
		t.Errorf("HEAD: %d %q, want the same 302 as GET", w.Code, loc)
	}
}

// Nothing on sale (no catalog: self-hosted), or a plan with no trial the
// clip could honestly invite to, or no clip at all: the 404 of before.
func TestPaywallWithoutAnOfferIsThe404OfBefore(t *testing.T) {
	cases := map[string]resolveCase{
		"no offer":                {promo: nil, clips: clipsIn(t, "en")},
		"a plan without a trial":  {promo: &offer.Offer{Tier: "silver", PeriodDays: 30, URL: "https://checkout.example/silver"}, clips: clipsIn(t, "en")},
		"a trial with no clip":    {promo: trialPlan, clips: clipsIn(t)},
		"only a clip in another":  {promo: trialPlan, clips: clipsIn(t, "ru"), accountLang: lang("de")},
		"no clips directory read": {promo: trialPlan, clips: nil},
	}
	for name, rc := range cases {
		t.Run(name, func(t *testing.T) {
			rc.res = paywalled()
			w := resolveWith(t, rc)
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d (Location %q), want 404", w.Code, w.Header().Get("Location"))
			}
		})
	}
}

// Everything that is not the paywall keeps its answer: a paid (or debrid)
// link redirects to the backend, an empty one is a 404 — not the clip, even
// with a plan on sale — and a failure is a 500.
func TestNonPaywallOutcomesAreUnchanged(t *testing.T) {
	clips := clipsIn(t, "en")
	t.Run("resolved", func(t *testing.T) {
		w := resolveWith(t, resolveCase{res: &fakeResolver{res: &co.LinkResult{URL: "https://seeder.example/file.mkv"}}, promo: trialPlan, clips: clips})
		if w.Code != http.StatusFound || w.Header().Get("Location") != "https://seeder.example/file.mkv" {
			t.Errorf("%d %q, want 302 to the backend URL", w.Code, w.Header().Get("Location"))
		}
	})
	t.Run("resolved under HEAD", func(t *testing.T) {
		w := resolveWith(t, resolveCase{res: &fakeResolver{res: &co.LinkResult{URL: "https://seeder.example/file.mkv"}}, promo: trialPlan, clips: clips, method: http.MethodHead})
		if w.Code != http.StatusFound || w.Header().Get("Location") != "https://seeder.example/file.mkv" {
			t.Errorf("%d %q, want 302 to the backend URL", w.Code, w.Header().Get("Location"))
		}
	})
	for name, res := range map[string]*fakeResolver{
		"no result":    {},
		"an empty URL": {res: &co.LinkResult{}},
	} {
		t.Run(name, func(t *testing.T) {
			w := resolveWith(t, resolveCase{res: res, promo: trialPlan, clips: clips})
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d (Location %q), want 404", w.Code, w.Header().Get("Location"))
			}
		})
	}
	t.Run("a failure", func(t *testing.T) {
		w := resolveWith(t, resolveCase{res: &fakeResolver{err: errors.New("rest-api down")}, promo: trialPlan, clips: clips})
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
	})
	t.Run("a wrapped paywall is still the paywall", func(t *testing.T) {
		w := resolveWith(t, resolveCase{res: &fakeResolver{err: errors.Wrap(lr.ErrPlanRequired, "context")}, promo: trialPlan, clips: clips})
		if w.Code != http.StatusFound {
			t.Errorf("status = %d, want 302 to the clip", w.Code)
		}
	})
}

// A re-rendered clip must be a new URL, or the CDN keeps serving the old
// one: the query carries a digest of the file.
func TestClipURLChangesWithTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paywall-en.mp4")
	url := func() string {
		p, err := loadPaywallClips(dir, "https://webtor.io/")
		if err != nil {
			t.Fatal(err)
		}
		u, _, ok := p.pick(trialPlan, "en")
		if !ok {
			t.Fatal("no clip picked")
		}
		return u
	}
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := url()
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := url(); second == first {
		t.Errorf("both renders give %q", first)
	}
	if !strings.HasPrefix(first, "https://webtor.io/pub/stremio/paywall-en.mp4?v=") {
		t.Errorf("URL = %q: the domain's trailing slash must not double", first)
	}
}

func TestMissingClipsDirectoryIsNoClips(t *testing.T) {
	p, err := loadPaywallClips(filepath.Join(t.TempDir(), "absent"), "https://webtor.io")
	if err != nil {
		t.Fatalf("a deployment without clips must start: %v", err)
	}
	if _, _, ok := p.pick(trialPlan, "en"); ok {
		t.Error("picked a clip that does not exist")
	}
}
