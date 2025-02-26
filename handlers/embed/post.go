package embed

import (
	"encoding/json"
	"github.com/webtor-io/web-ui/services/models"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/job"
)

type PostArgs struct {
	ID            string
	EmbedSettings *models.EmbedSettings
}

type PostData struct {
	ID             string
	EmbedSettings  *models.EmbedSettings
	DomainSettings *embed.DomainSettingsData
	Job            *job.Job
}

func (s *Handler) bindPostArgs(c *gin.Context) (*PostArgs, error) {
	rawSettings := c.PostForm("settings")
	var settings models.EmbedSettings
	err := json.Unmarshal([]byte(rawSettings), &settings)
	if err != nil {
		return nil, err
	}
	id := c.Query("id")

	return &PostArgs{
		ID:            id,
		EmbedSettings: &settings,
	}, nil

}

func (s *Handler) post(c *gin.Context) {
	tpl := s.tb.Build("embed/post")
	pd := PostData{}
	args, err := s.bindPostArgs(c)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.ID = args.ID
	u, err := url.Parse(args.EmbedSettings.Referer)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	dsd, err := s.ds.Get(u.Hostname())
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.EmbedSettings = args.EmbedSettings
	pd.DomainSettings = dsd
	embedJob, err := s.jobs.Embed(web.NewContext(c), s.cl, args.EmbedSettings, dsd)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.Job = embedJob
	tpl.HTML(http.StatusAccepted, web.NewContext(c).WithData(pd))
}
