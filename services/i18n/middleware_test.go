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

// Accept-Language must never redirect, on any bare path: locale-adaptive
// crawls that get 302 /{lang}/ never see the canonical English pages, the
// hreflang cluster stops consolidating, and localized pages surface in
// foreign SERPs with near-zero CTR (8.8K US impressions → 17 clicks/week,
// home and tool pages alike). Bare URLs answer 200 for every first-time
// visitor; the language offer is the banner's job. A stored cookie
// preference still redirects everywhere — cookies are personal state
// crawlers do not carry. And no cookie may be written for non-English
// browsers: the banner must keep appearing until the visitor chooses.
func TestAcceptLanguageNeverRedirects(t *testing.T) {
	h := langEngine(t, nil)

	for _, path := range []string{"/", "/some-page"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "example.com"
		req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || w.Body.String() != path {
			t.Errorf("%s with es Accept-Language: got %d → %q, want 200 serving %s",
				path, w.Code, w.Header().Get("Location"), path)
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == langCookie {
				t.Errorf("%s with es Accept-Language: lang cookie %q written, want none (kills the banner)", path, c.Value)
			}
		}
	}

	// English-preferring browsers get the default cookie so Accept-Language
	// isn't re-parsed on every request.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	got := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == langCookie {
			got = c.Value
		}
	}
	if w.Code != http.StatusOK || got != DefaultLang {
		t.Errorf("/ with en-first Accept-Language: got %d cookie %q, want 200 with cookie %q", w.Code, got, DefaultLang)
	}

	// An explicit cookie preference still wins, on the homepage and nested.
	if w := langGet(h, "example.com", "/", "es"); w.Code != http.StatusFound ||
		w.Header().Get("Location") != "/es/" {
		t.Errorf("/ with es cookie: got %d → %q, want 302 → /es/", w.Code, w.Header().Get("Location"))
	}
	if w := langGet(h, "example.com", "/some-page", "es"); w.Code != http.StatusFound ||
		w.Header().Get("Location") != "/es/some-page" {
		t.Errorf("/some-page with es cookie: got %d → %q, want 302 → /es/some-page", w.Code, w.Header().Get("Location"))
	}
}
