package legal

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// aliases are the legal URLs that exist only as a redirect: a name that gets
// requested but the page has never had. 301 because the target does not
// depend on anything but the URL (the language comes from its prefix; a bare
// URL with a language cookie is redirected before routing).
var aliases = map[string]string{
	"/terms": "/tos",
}

type Handler struct {
	tb      template.Builder[*web.Context]
	hasView func(name string) bool
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context]) {
	h := &Handler{
		tb:      tm.MustRegisterViews("legal/**/*").WithLayout("main"),
		hasView: tm.HasView,
	}

	r.GET("/legal/*template", h.get)
}

type Data struct {
}

func (s *Handler) get(c *gin.Context) {
	page := c.Param("template")
	if to, ok := aliases[page]; ok {
		c.Redirect(http.StatusMovedPermanently, web.LangURL(i18n.GetLang(c), "/legal"+to))
		return
	}
	// The page name comes from the URL; an unknown one used to reach the
	// template manager and come back as a bare 500 (/legal/terms, /legal/).
	if !s.hasView("legal" + page) {
		s.tb.Build("error/page").HTML(http.StatusNotFound, web.NewContext(c).WithErrKey("error.page_not_found"))
		return
	}
	s.tb.Build("legal"+page).HTML(http.StatusOK, web.NewContext(c).WithData(&Data{}))
}
