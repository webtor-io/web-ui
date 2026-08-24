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

// The ?lang switch must be a permanent redirect (so search engines drop the
// parametric duplicate — /?lang=en collected 2.5K impressions/week as a
// separately indexed URL under a 302) but must never be browser-cached (a
// cached 301 skips the Set-Cookie on the next switch, stranding the user in
// the previous language).
func TestLangQuerySwitchRedirectsPermanentlyUncached(t *testing.T) {
	h := langEngine(t, nil)

	// Bare path + ?lang=en (the sitewide switcher link), previous pref ru.
	w := langGet(h, "example.com", "/?lang=en", "ru")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/" {
		t.Errorf("/?lang=en: got %d → %q, want 301 → /", w.Code, w.Header().Get("Location"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("/?lang=en: Cache-Control = %q, want no-store", cc)
	}
	var langSet string
	for _, c := range w.Result().Cookies() {
		if c.Name == langCookie {
			langSet = c.Value
		}
	}
	if langSet != "en" {
		t.Errorf("/?lang=en: lang cookie set to %q, want en", langSet)
	}

	// Switching to EN from a prefixed page strips the prefix.
	if w := langGet(h, "example.com", "/ru/some-page?lang=en", "ru"); w.Code != http.StatusMovedPermanently ||
		w.Header().Get("Location") != "/some-page" {
		t.Errorf("/ru/some-page?lang=en: got %d → %q, want 301 → /some-page", w.Code, w.Header().Get("Location"))
	}

	// Switching to a non-default language adds the prefix.
	if w := langGet(h, "example.com", "/some-page?lang=ru", ""); w.Code != http.StatusMovedPermanently ||
		w.Header().Get("Location") != "/ru/some-page" {
		t.Errorf("/some-page?lang=ru: got %d → %q, want 301 → /ru/some-page", w.Code, w.Header().Get("Location"))
	}
}

// The legacy /en/ prefix redirect also flips the cookie, so it must not be
// browser-cached either.
func TestEnPrefixRedirectUncached(t *testing.T) {
	h := langEngine(t, nil)
	w := langGet(h, "example.com", "/en/some-page", "ru")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/some-page" {
		t.Errorf("/en/some-page: got %d → %q, want 301 → /some-page", w.Code, w.Header().Get("Location"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("/en/some-page: Cache-Control = %q, want no-store", cc)
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

// The bare homepage must never Accept-Language-redirect: locale-adaptive
// crawls that get 302 /{lang}/ never see /, the language cluster stops
// consolidating, and localized homepages surface in the US SERP with
// near-zero CTR (8.6K impressions → 15 clicks, week of 2026-08-10). / is the
// x-default entry point and has to answer 200 for every first-time visitor.
// A stored cookie preference still redirects — cookies are personal state
// crawlers do not carry.
func TestBareHomepageServesDefaultDespiteAcceptLanguage(t *testing.T) {
	h := langEngine(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "/" {
		t.Errorf("/ with es Accept-Language: got %d → %q, want 200 serving /",
			w.Code, w.Header().Get("Location"))
	}

	// Nested bare paths keep the first-visit detection redirect.
	req = httptest.NewRequest(http.MethodGet, "/some-page", nil)
	req.Host = "example.com"
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/es/some-page" {
		t.Errorf("/some-page with es Accept-Language: got %d → %q, want 302 → /es/some-page",
			w.Code, w.Header().Get("Location"))
	}

	// An explicit cookie preference still wins on the homepage.
	if w := langGet(h, "example.com", "/", "es"); w.Code != http.StatusFound ||
		w.Header().Get("Location") != "/es/" {
		t.Errorf("/ with es cookie: got %d → %q, want 302 → /es/", w.Code, w.Header().Get("Location"))
	}
}
