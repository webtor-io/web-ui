package instructions

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
	h := &Handler{
		tb: tm.MustRegisterViews("instructions/**/*").WithLayout("main"),
	}

	r.GET("/instructions/*template", h.get)
}

type Data struct {
}

func (s *Handler) get(c *gin.Context) {
	s.tb.Build("instructions"+c.Param("template")).HTML(http.StatusOK, web.NewContext(c).WithData(&Data{}))
}
