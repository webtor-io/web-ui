package user_settings

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
)

// The account's language is learned here and nowhere else — this is the
// only place that sees both a signed-in user and a resolved language on
// every request, and the notification cron has no other way to know it.
//
// Two rules matter. Write only when it changed, because this runs on every
// page and an UPDATE per request would be pure waste. And write only when
// the request actually went through language routing: the dedicated API
// hosts skip it and default to English, so learning from them would rewrite
// a Russian account's language on its next API call.

type fakeSettings struct {
	row      *models.UserSettings
	getErr   error
	written  []string
	writeErr error
}

func (f *fakeSettings) Get(context.Context, uuid.UUID) (*models.UserSettings, error) {
	return f.row, f.getErr
}

func (f *fakeSettings) SetLang(_ context.Context, _ uuid.UUID, lang string) error {
	f.written = append(f.written, lang)
	return f.writeErr
}

// run drives one request through the real i18n middleware and this one.
// langHeader is what the HTTP-level middleware sets from a URL prefix;
// empty means the request never went through language routing at all.
func run(t *testing.T, svc settings, langHeader string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(i18n.GinMiddleware(i18n.New(locales.FS())))
	r.Use(Middleware(svc))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/?token=test", nil)
	if langHeader != "" {
		req.Header.Set(i18n.LangHeader, langHeader)
	}
	if signedIn {
		req = req.WithContext(context.WithValue(req.Context(),
			auth.UserContext{}, &models.User{UserID: uuid.NewV4(), Email: "viewer@example.com"}))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLanguageIsStoredWhenItChanges(t *testing.T) {
	en := "en"
	svc := &fakeSettings{row: &models.UserSettings{Lang: &en}}
	run(t, svc, "ru", true)

	if len(svc.written) != 1 || svc.written[0] != "ru" {
		t.Errorf("written: %v, want one write of \"ru\"", svc.written)
	}
}

func TestLanguageIsNotRewrittenWhenUnchanged(t *testing.T) {
	ru := "ru"
	svc := &fakeSettings{row: &models.UserSettings{Lang: &ru}}
	run(t, svc, "ru", true)

	if len(svc.written) != 0 {
		t.Errorf("written: %v, want nothing — the language did not change", svc.written)
	}
}

// An account that has never been seen since the column was added has no
// language at all; the first prefixed request fills it in.
func TestFirstSightingStoresTheLanguage(t *testing.T) {
	svc := &fakeSettings{row: &models.UserSettings{}}
	run(t, svc, "de", true)

	if len(svc.written) != 1 || svc.written[0] != "de" {
		t.Errorf("written: %v, want one write of \"de\"", svc.written)
	}
}

// A request that never went through language routing — an API host, where
// the prefix middleware is skipped entirely — resolves to English by
// default. Learning from it would silently anglicise every account that
// uses the API.
func TestApiHostRequestDoesNotOverwriteTheLanguage(t *testing.T) {
	ru := "ru"
	svc := &fakeSettings{row: &models.UserSettings{Lang: &ru}}
	run(t, svc, "", true)

	if len(svc.written) != 0 {
		t.Errorf("written: %v, want nothing from a request that bypassed language routing", svc.written)
	}
}

func TestAnonymousRequestsWriteNothing(t *testing.T) {
	svc := &fakeSettings{row: &models.UserSettings{}}
	run(t, svc, "ru", false)

	if len(svc.written) != 0 {
		t.Errorf("written: %v, want nothing for an anonymous request", svc.written)
	}
}

// A settings read that fails takes the language write with it, but must not
// take the page down.
func TestReadFailureIsSurvivable(t *testing.T) {
	svc := &fakeSettings{getErr: errors.New("db down")}
	w := run(t, svc, "ru", true)

	if len(svc.written) != 0 {
		t.Errorf("written: %v, want nothing when the row could not be read", svc.written)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

// The write is best-effort: losing it costs one notification in the
// previous language, which is not worth failing a render over.
func TestWriteFailureIsSurvivable(t *testing.T) {
	svc := &fakeSettings{row: &models.UserSettings{}, writeErr: errors.New("db down")}
	w := run(t, svc, "ru", true)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 — a failed language write must not break the page", w.Code)
	}
}
