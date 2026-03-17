package tests

import (
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

type Handler struct {
	tb template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context]) {
	if !gin.IsDebugging() {
		return
	}
	h := &Handler{
		tb: tm.MustRegisterViews("tests/**/*").WithLayout("main"),
	}

	r.GET("/tests/*template", h.get)
}

type Data struct {
}

func (s *Handler) get(c *gin.Context) {
	s.tb.Build("tests"+c.Param("template")).HTML(http.StatusOK, web.NewContext(c).WithData(&Data{}))
}
