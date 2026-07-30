package web

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
)

// The whole point of the resolver indirection: the middleware is global, so a
// poster image or Stremio poll must not pay for a checklist nobody renders.
func TestOnboardingResolvesOnlyWhenAsked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/lib/poster/abc/240.jpg", nil)

	calls := 0
	want := &models.OnboardingChecklist{Done: 1, Total: 3}
	SetOnboardingResolver(c, func() *models.OnboardingChecklist {
		calls++
		return want
	})
	if calls != 0 {
		t.Fatalf("registering the resolver must not query, got %d calls", calls)
	}

	if got := (&Context{ginCtx: c}).Onboarding(); got != want {
		t.Fatalf("expected the resolved checklist, got %+v", got)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one resolve, got %d", calls)
	}

	// Memoised on the gin context, so the navbar counter and the home card
	// share one query even though handlers build several Contexts per request.
	if got := (&Context{ginCtx: c}).Onboarding(); got != want {
		t.Fatal("a second Context must see the memoised value")
	}
	if calls != 1 {
		t.Fatalf("expected the result to be memoised, got %d calls", calls)
	}
}

// Routes mounted before the middleware, and any Context built without gin, must
// read as "nothing to show" rather than panic.
func TestOnboardingWithoutResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := (&Context{ginCtx: c}).Onboarding(); got != nil {
		t.Errorf("expected nil without a resolver, got %+v", got)
	}
	if got := (&Context{}).Onboarding(); got != nil {
		t.Errorf("expected nil without a gin context, got %+v", got)
	}
}

// Templates address Onboarding exactly as they did when it was a field. Pinned
// against the real Context: nav.html renders `{{ if .Onboarding }}` and
// `{{ .Onboarding.Done }}`, and a method Go templates refused to call would
// silently render an empty navbar on every page.
func TestOnboardingReadsLikeAFieldInTemplates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	SetOnboardingResolver(c, func() *models.OnboardingChecklist {
		return &models.OnboardingChecklist{Done: 2, Total: 3}
	})

	tmpl := template.Must(template.New("t").Parse(`{{ if .Onboarding }}{{ .Onboarding.Done }}/{{ .Onboarding.Total }}{{ else }}none{{ end }}`))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &Context{ginCtx: c}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if buf.String() != "2/3" {
		t.Fatalf("want 2/3, got %q", buf.String())
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/", nil)
	SetOnboardingResolver(c2, func() *models.OnboardingChecklist { return nil })
	buf.Reset()
	if err := tmpl.Execute(&buf, &Context{ginCtx: c2}); err != nil {
		t.Fatalf("execute nil: %v", err)
	}
	if buf.String() != "none" {
		t.Fatalf("want none, got %q", buf.String())
	}
}
