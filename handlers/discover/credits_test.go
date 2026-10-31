package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
)

// langCredits is a credits and localize mapper that records, in order, what
// it is asked in which language.
type langCredits struct{ calls []string }

func (f *langCredits) Map(context.Context, *models.VideoContent, models.ContentType, bool) (*models.VideoMetadata, error) {
	return nil, nil
}
func (f *langCredits) GetName() string { return "fake" }
func (f *langCredits) Localize(ctx context.Context, _ string, lang string) (string, string, error) {
	call := "localize " + lang
	if _, ok := ctx.Deadline(); ok {
		call += " bounded"
	}
	f.calls = append(f.calls, call)
	return "Фильм А", "", nil
}
func (f *langCredits) Credits(_ context.Context, _ string, _ models.ContentType, lang string) (*enrich.Credits, error) {
	f.calls = append(f.calls, "credits "+lang)
	return &enrich.Credits{Directors: []string{"Режиссёр А"}}, nil
}

// Credits asks in the page language, after Localize has stored the row with
// names; English has nothing to localize.
func TestCreditsAsksInThePageLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &langCredits{}
	h := &Handler{en: enrich.NewEnricher(nil, nil, []enrich.MetadataMapper{fake}, nil, nil)}
	r := gin.New()
	r.Use(i18n.GinMiddleware(i18n.New(os.DirFS("../../locales"))))
	r.POST("/discover/credits", h.credits)
	for _, routed := range []string{"ru", "pt", ""} {
		req := httptest.NewRequest(http.MethodPost, "/discover/credits", strings.NewReader(`{"id":"tt1","type":"movie"}`))
		req.Header.Set("Content-Type", "application/json")
		if routed != "" {
			req.Header.Set(i18n.LangHeader, routed)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Режиссёр А") {
			t.Errorf("%q: %d %s", routed, w.Code, w.Body)
		}
	}
	if want := []string{"localize ru bounded", "credits ru", "localize pt bounded", "credits pt", "credits en"}; !reflect.DeepEqual(fake.calls, want) {
		t.Errorf("calls %v, want %v", fake.calls, want)
	}
}
