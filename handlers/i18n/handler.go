package i18n

import (
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/i18n"
	w "github.com/webtor-io/web-ui/services/web"
)

// RegisterHandler sets up i18n support:
//   - HTTP middleware on Web for URL prefix stripping (runs before Gin routing)
//   - Gin middleware for language context resolution (Accept-Language, cookie)
func RegisterHandler(r *gin.Engine, web *w.Web, svc *i18n.Service) {
	// HTTP-level middleware: strips /ru/, /es/, /de/ prefix from URL path,
	// redirects /en/* → /*. Must run before Gin routing.
	web.Use(i18n.HTTPMiddleware)

	// Gin-level middleware: resolves language from X-Lang header (set by HTTP
	// middleware), cookie, or Accept-Language. Stores lang + Localizer in context.
	r.Use(i18n.GinMiddleware(svc))
}
