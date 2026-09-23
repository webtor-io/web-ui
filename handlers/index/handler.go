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
	currentTool := common.ToolByURL(instruction)

	// A form that failed sends the visitor back here with ?err= (see
	// web.RedirectWithErrorAndPath). That URL is the state of one visit, not
	// a page: indexed, it is the home page again under a URL that carries a
	// complaint, and the stale resource links that used to land here filled a
	// search index with it. The canonical still points at the clean URL.
	if c.Query("err") != "" {
		web.Noindex(c)
	}
	errKey := ""
	if c.Query("status") == "error" && c.Query("err") != "" {
		errKey = c.Query("err")
	}

	Render(c, s.tb, s.pg, http.StatusOK, &Data{
		Instruction: instruction,
		Tool:        currentTool,
	}, errKey)
}

// Render answers with the home page (or a tool page, when data.Tool is set)
// under the given status, errKey (when set) shown above the form. GET / is its
// 200; a resource URL that names nothing is its 404 (handlers/resource), which
// is the same page a visitor used to reach through a redirect to /?err=, now
// at the URL they asked for.
//
// tb must be able to build "index": this package registers the view, so a
// caller from another package relies on RegisterHandler having run, the same
// way handlers/resource's POST already does.
func Render(c *gin.Context, tb template.Builder[*web.Context], pg *cs.PG, status int, data *Data, errKey string) {
	// Continue-watching is home-page only: tool pages are SEO landings and
	// carry their own CTA.
	if data.Tool == nil {
		user := auth.GetUserFromContext(c)
		if user.HasAuth() {
			if db := pg.Get(); db != nil {
				data.ContinueWatching, _ = models.GetRecentlyWatched(c.Request.Context(), db, user.ID, 10)
			}
		}
	}

	ctx := web.NewContext(c).WithData(data)
	// The checklist is loaded by the onboarding middleware for every page (the
	// navbar counter needs it); the home page just renders the full card from
	// the same value, so both always agree.
	if data.Tool == nil {
		data.Onboarding = ctx.Onboarding()
	}

	if errKey != "" {
		ctx = ctx.WithErrKey(errKey)
	}

	tb.Build("index").HTML(status, ctx)
}
