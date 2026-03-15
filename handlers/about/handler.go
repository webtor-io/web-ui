package about

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
		tb: tm.MustRegisterViews("about/*").WithLayout("main"),
	}

	r.GET("/about", h.get)
}

type Data struct {
}

func (s *Handler) get(c *gin.Context) {
	s.tb.Build("about/index").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{}))
}
