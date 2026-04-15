package i18n

import (
	"strings"

	"github.com/gin-gonic/gin"
	goI18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	contextKeyLang      = "i18n.lang"
	contextKeyLocalizer = "i18n.localizer"
)

// GinMiddleware returns a Gin middleware that resolves the current language
// and stores it (plus a Localizer) in the gin.Context for downstream use.
//
// The language is determined ONLY by the URL prefix (set as X-Lang header
// by the HTTP middleware). If no prefix is present the page is canonical
// English — Accept-Language and cookies do NOT override this, because
// search engines must see a stable lang per URL.
//
// Accept-Language is used solely to decide whether to suggest a redirect
// to the user's preferred language (see BrowserLang).
func GinMiddleware(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := DefaultLang
		if h := c.GetHeader(LangHeader); h != "" && IsSupported(h) {
			lang = h
		}
		loc := svc.Localizer(lang)
		c.Set(contextKeyLang, lang)
		c.Set(contextKeyLocalizer, loc)
		c.Next()
	}
}

// BrowserLang returns the user's preferred language from Accept-Language
// header, but only if it differs from the default and comes before English
// in the preference list. Returns "" if the browser prefers English or no
// supported language is detected.
// Useful for showing a "switch to Russian?" banner or auto-redirecting.
func BrowserLang(c *gin.Context) string {
	accept := c.GetHeader("Accept-Language")
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

// T translates a message key using the language from gin.Context.
// Convenient shorthand for handlers: i18n.T(c, "toast.added")
func T(c *gin.Context, key string) string {
	return TranslateWithLocalizer(GetLocalizer(c), key)
}

// GetLang returns the resolved language code from the gin.Context.
func GetLang(c *gin.Context) string {
	if v, ok := c.Get(contextKeyLang); ok {
		return v.(string)
	}
	return DefaultLang
}

// GetLocalizer returns the Localizer from the gin.Context.
func GetLocalizer(c *gin.Context) *goI18n.Localizer {
	if v, ok := c.Get(contextKeyLocalizer); ok {
		return v.(*goI18n.Localizer)
	}
	return nil
}
