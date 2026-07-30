package index

import (
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
	Onboarding       *models.OnboardingChecklist
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
	indexable := r.Group("", web.IndexFollow())
	indexable.GET("/", h.index)
	for _, tool := range common.Tools {
		indexable.GET("/"+tool.Url, h.index)
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

	// Continue-watching is home-page only: tool pages are SEO landings and
	// carry their own CTA.
	if currentTool == nil {
		user := auth.GetUserFromContext(c)
		if user.HasAuth() {
			if db := s.pg.Get(); db != nil {
				data.ContinueWatching, _ = models.GetRecentlyWatched(c.Request.Context(), db, user.ID, 10)
			}
		}
	}

	ctx := web.NewContext(c).WithData(data)
	// The checklist is loaded by the onboarding middleware for every page (the
	// navbar counter needs it); the home page just renders the full card from
	// the same value, so both always agree.
	if currentTool == nil {
		data.Onboarding = ctx.Onboarding()
	}

	if c.Query("status") == "error" && c.Query("err") != "" {
		ctx = ctx.WithErrKey(c.Query("err"))
	}

	s.tb.Build("index").HTML(http.StatusOK, ctx)
}
