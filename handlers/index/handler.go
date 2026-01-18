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
	Tool        *common.Tool
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
	instruction := strings.TrimPrefix(c.Request.URL.Path, "/")

	// Find the matching tool based on the current URL
	var currentTool *common.Tool
	for i := range common.Tools {
		if common.Tools[i].Url == instruction {
			currentTool = &common.Tools[i]
			break
		}
	}

	s.tb.Build("index").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		Instruction: instruction,
		Tool:        currentTool,
	}))
}
