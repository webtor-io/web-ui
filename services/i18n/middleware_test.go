package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func langEngine(t *testing.T, skipHosts []string) http.Handler {
	t.Helper()
	// SupportedLangs is wired at runtime by New(); seed it here so the
	// redirect rules have languages to work with.
	prev := SupportedLangs
	SupportedLangs = []string{"en", "ru", "es"}
	t.Cleanup(func() { SupportedLangs = prev })
	return HTTPMiddleware(skipHosts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
}

func langGet(h http.Handler, host, path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: langCookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// A dedicated API host must never language-redirect: with a RU cookie the docs
// page on api.<domain> used to 302 onto /ru/v1/docs/..., which is not a page.
func TestAPIHostBypassesLanguageRouting(t *testing.T) {
	h := langEngine(t, []string{"api.example.com"})

	if w := langGet(h, "api.example.com", "/v1/docs/index.html", "ru"); w.Code != http.StatusOK {
		t.Errorf("api host with ru cookie: got %d (Location %q), want 200 pass-through",
			w.Code, w.Header().Get("Location"))
	}
	// Port present (local runs off 443) must not defeat the match.
	if w := langGet(h, "api.example.com:8080", "/v1/library", "ru"); w.Code != http.StatusOK {
		t.Errorf("api host with port: got %d, want 200 pass-through", w.Code)
	}
	// The main host keeps its normal behaviour.
	if w := langGet(h, "example.com", "/some-page", "ru"); w.Code != http.StatusFound ||
		w.Header().Get("Location") != "/ru/some-page" {
		t.Errorf("main host: got %d → %q, want 302 → /ru/some-page", w.Code, w.Header().Get("Location"))
	}
}

// /api/... and /api-credentials/... are API surface on the main host: JSON
// consumers (the Swagger prefill fetch among them) must not be bounced through
// a language redirect.
func TestAPIPathsBypassLanguageRouting(t *testing.T) {
	h := langEngine(t, nil)
	for _, p := range []string{"/api/v1/docs/index.html", "/api-credentials/key"} {
		if w := langGet(h, "example.com", p, "ru"); w.Code != http.StatusOK {
			t.Errorf("%s with ru cookie: got %d, want 200 pass-through", p, w.Code)
		}
	}
}
