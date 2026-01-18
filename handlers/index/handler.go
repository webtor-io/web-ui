package index

import (
	"net/http"
	"strings"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

type Data struct {
	Instruction string
}

type Handler struct {
	tb template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context]) {
	h := &Handler{
		tb: tm.MustRegisterViews("*").WithLayout("main"),
	}
	r.GET("/", h.index)
	for _, tool := range common.Tools {
		r.GET("/"+tool.Url, h.index)
	}
}

func (s *Handler) index(c *gin.Context) {
	s.tb.Build("index").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		Instruction: strings.TrimPrefix(c.Request.URL.Path, "/"),
	}))
}
