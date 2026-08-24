package i18n

import (
	"net/http"
	"strings"

	"golang.org/x/text/language"
)

const (
	LangHeader = "X-Lang"
	langCookie = "lang"
)

// HTTPMiddleware wraps an http.Handler to handle language routing:
//
//  1. ?lang=X query (from the language switcher) → set cookie=X and
//     301-redirect (no-store) to the canonical URL for X (with ?lang stripped)
//  2. /en/* → 301 redirect to /* (English is default, no prefix; cookie→en)
//  3. /{lang}/* → strip prefix, set X-Lang header, set lang cookie
//  4. No prefix + no cookie → serve English and, for browsers preferring a
//     supported non-English language, let the layout offer a switch via the
//     language-suggest banner. Accept-Language NEVER redirects: bare URLs
//     are the canonical English pages and the x-default crawl surface
//  5. No prefix + cookie has non-default language → 302 redirect to
//     /{cookie}/path so external entry points (OAuth callbacks, bookmarks,
//     shared links) honour the user's language preference
//  6. No prefix + cookie has default language → serve English
//
// skipHosts are hostnames whose requests bypass language routing entirely —
// the dedicated API hosts (api.webtor.io), where a language cookie must never
// 302 a docs page or an API call onto a /ru/ URL.
func HTTPMiddleware(skipHosts []string) func(http.Handler) http.Handler {
	supported := make(map[string]bool, len(SupportedLangs))
	for _, l := range SupportedLangs {
		supported[l] = true
	}
	skip := make(map[string]bool, len(skipHosts))
	for _, h := range skipHosts {
		skip[strings.ToLower(h)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip[requestHost(r)] {
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path

			// Static and API paths are never language-prefixed.
			if strings.HasPrefix(path, "/assets/") ||
				strings.HasPrefix(path, "/api/") ||
				strings.HasPrefix(path, "/api-credentials/") ||
				strings.HasPrefix(path, "/pub/") ||
				strings.HasPrefix(path, "/s/") ||
				strings.HasPrefix(path, "/token/") ||
				strings.HasPrefix(path, "/embed/") ||
				strings.HasPrefix(path, "/stremio/") ||
				strings.HasPrefix(path, "/user-subtitle/file/") ||
				strings.HasPrefix(path, "/lib/poster/") ||
				strings.HasPrefix(path, "/lib/movie/poster/") ||
				strings.HasPrefix(path, "/lib/series/poster/") ||
				strings.HasPrefix(path, "/lib/episode/still/") {
				next.ServeHTTP(w, r)
				return
			}

			// Only GET/HEAD go through language redirects. POST/PUT/DELETE/PATCH
			// targets are state-mutating endpoints (form actions like /lib/add)
			// where a 302 would drop the request body in most browsers. They
			// still get prefix-stripping so handlers see the same URL shape.
			isSafe := r.Method == http.MethodGet || r.Method == http.MethodHead

			// ?lang=X — explicit switch from the language dropdown (step 1).
			// The switcher renders as <a>, so only GET requests reach this
			// branch in practice. Set the cookie and redirect to the canonical
			// URL for that language with the query stripped. The follow-up
			// request goes through the normal prefix / no-prefix branches below.
			if reqLang := r.URL.Query().Get("lang"); reqLang != "" && isSafe {
				if supported[reqLang] {
					http.SetCookie(w, &http.Cookie{
						Name:     langCookie,
						Value:    reqLang,
						Path:     "/",
						MaxAge:   365 * 24 * 3600,
						SameSite: http.SameSiteLaxMode,
					})
					q := r.URL.Query()
					q.Del("lang")
					// Build the canonical path for the requested language:
					// strip any existing language prefix, then re-add if needed.
					basePath := path
					if len(basePath) >= 3 && basePath[0] == '/' {
						seg := basePath[1:3]
						if supported[seg] && (len(basePath) == 3 || basePath[3] == '/') {
							basePath = basePath[3:]
							if basePath == "" {
								basePath = "/"
							}
						}
					}
					target := basePath
					if reqLang != DefaultLang {
						if basePath == "/" {
							target = "/" + reqLang + "/"
						} else {
							target = "/" + reqLang + basePath
						}
					}
					if encoded := q.Encode(); encoded != "" {
						target += "?" + encoded
					}
					// 301 so search engines drop the parametric duplicate
					// (?lang=en collected impressions as a separate URL under a
					// 302, which keeps the source URL indexed). no-store because
					// browsers cache permanent redirects: a cached 301 would skip
					// the server — and the Set-Cookie above — on the next switch.
					w.Header().Set("Cache-Control", "no-store")
					http.Redirect(w, r, target, http.StatusMovedPermanently)
					return
				}
			}

			// Try to extract a two-letter language code from the first path segment.
			if len(path) >= 3 && path[0] == '/' {
				seg := path[1:3]
				rest := path[3:]

				if (rest == "" || rest[0] == '/') && supported[seg] {
					// /en/* → redirect to /* (canonical English has no prefix).
					// Also flip the cookie to English so the next bare-path
					// visit doesn't auto-redirect back to the old preference.
					if seg == DefaultLang {
						http.SetCookie(w, &http.Cookie{
							Name:     langCookie,
							Value:    DefaultLang,
							Path:     "/",
							MaxAge:   365 * 24 * 3600,
							SameSite: http.SameSiteLaxMode,
						})
						target := rest
						if target == "" {
							target = "/"
						}
						if r.URL.RawQuery != "" {
							target += "?" + r.URL.RawQuery
						}
						// no-store: a browser-cached 301 would skip the
						// cookie-flip above on a later visit to the same
						// /en/ URL, stranding the user in the old language.
						w.Header().Set("Cache-Control", "no-store")
						http.Redirect(w, r, target, http.StatusMovedPermanently)
						return
					}

					// Remember language preference in cookie.
					http.SetCookie(w, &http.Cookie{
						Name:     langCookie,
						Value:    seg,
						Path:     "/",
						MaxAge:   365 * 24 * 3600,
						SameSite: http.SameSiteLaxMode,
					})

					// Strip the prefix and set the language header.
					r.Header.Set(LangHeader, seg)
					r.URL.Path = rest
					if r.URL.Path == "" {
						r.URL.Path = "/"
					}
					r.URL.RawPath = ""
					next.ServeHTTP(w, r)
					return
				}
			}

			// No language prefix.
			cookie, err := r.Cookie(langCookie)
			if err != nil {
				// No cookie → first visit. Accept-Language never causes a
				// redirect: bare URLs are the canonical English pages and the
				// x-default crawl surface, and the old auto-302 by browser
				// language hid them from locale-adaptive crawls — hreflang
				// clusters stopped consolidating and localized pages surfaced
				// in foreign SERPs with near-zero CTR (8.8K US impressions →
				// 17 clicks/week across home and tool pages). It also made
				// the language depend on the entry point once the homepage
				// alone stopped redirecting. First-time non-English visitors
				// get the language-suggest banner instead (web.Context →
				// lang_suggest_banner.html); NO cookie is written for them,
				// so the offer follows them across bare pages until they
				// choose. Browsers preferring English get the cookie so
				// Accept-Language isn't re-parsed on every request.
				if isSafe && detectBrowserLang(r) == "" {
					http.SetCookie(w, &http.Cookie{
						Name:     langCookie,
						Value:    DefaultLang,
						Path:     "/",
						MaxAge:   365 * 24 * 3600,
						SameSite: http.SameSiteLaxMode,
					})
				}
			} else if cookie != nil && cookie.Value != DefaultLang && IsSupported(cookie.Value) && isSafe {
				// Cookie indicates a non-default language preference. The user
				// arrived at a bare path (external OAuth callback, bookmark,
				// shared link) — honour their preference by redirecting to the
				// prefixed URL. Explicit English switches come through the
				// ?lang=en branch above and clear the cookie before reaching here.
				target := "/" + cookie.Value + path
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// requestHost normalizes the Host header to a bare lowercase hostname: a port
// is present whenever the service is off 443, and matching would silently fail
// without stripping it.
func requestHost(r *http.Request) string {
	h := r.Host
	if i := strings.LastIndexByte(h, ':'); i >= 0 && strings.IndexByte(h[i:], ']') < 0 {
		h = h[:i]
	}
	return strings.ToLower(h)
}

// detectBrowserLang returns the user's preferred supported non-English language
// from the Accept-Language header, or "" if English (the default) comes first
// in the preference list or no supported language is detected.
//
// The preferences are ordered — the loop stops at the first *supported* entry:
// if English wins, we return "" (no redirect). This way `en-US,ru;q=0.8` does
// not wrongly redirect a primarily-English user to /ru/.
func detectBrowserLang(r *http.Request) string {
	accept := r.Header.Get("Accept-Language")
	if accept == "" {
		return ""
	}
	tags, _, err := language.ParseAcceptLanguage(accept)
	if err != nil {
		return ""
	}
	for _, tag := range tags {
		base, _ := tag.Base()
		code := strings.ToLower(base.String())
		if !IsSupported(code) {
			continue
		}
		if code == DefaultLang {
			return ""
		}
		return code
	}
	return ""
}
