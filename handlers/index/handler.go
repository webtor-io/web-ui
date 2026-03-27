package index

import (
	"errors"
	"net/http"
	"strings"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

type Data struct {
	Instruction      string
	Tool             *common.Tool
	ContinueWatching []*models.WatchHistory
}

type Handler struct {
	tb template.Builder[*web.Context]
	pg *cs.PG
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		tb: tm.MustRegisterViews("*").WithLayout("main"),
		pg: pg,
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

	data := &Data{
		Instruction: instruction,
		Tool:        currentTool,
	}

	// Fetch continue watching for authenticated users
	if currentTool == nil {
		user := auth.GetUserFromContext(c)
		if user.HasAuth() {
			if db := s.pg.Get(); db != nil {
				data.ContinueWatching, _ = models.GetRecentlyWatched(c.Request.Context(), db, user.ID, 10)
			}
		}
	}

	ctx := web.NewContext(c).WithData(data)

	if c.Query("status") == "error" && c.Query("err") != "" {
		ctx = ctx.WithErr(errors.New(c.Query("err")))
	}

	s.tb.Build("index").HTML(http.StatusOK, ctx)
}
