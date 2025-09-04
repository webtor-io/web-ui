package action

import (
	"net/http"

	wj "github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
)

type PostArgs struct {
	ResourceID          string
	ItemID              string
	ApiClaims           *api.Claims
	UserClaims          *claims.Data
	Purge               bool
	VideoStreamUserData *models.VideoStreamUserData
}

type TrackPutArgs struct {
	ID         string `json:"id"`
	ResourceID string `json:"resourceID"`
	ItemID     string `json:"itemID"`
}

type PostData struct {
	Job  *job.Job
	Args *PostArgs
}

type Handler struct {
	jobs *wj.Handler
	tb   template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], jobs *wj.Handler) {
	h := &Handler{
		tb:   tm.MustRegisterViews("action/**/*").WithHelper(NewHelper()),
		jobs: jobs,
	}
	r.POST("/download-file", func(c *gin.Context) {
		h.post(c, "download")
	})
	r.POST("/download-dir", func(c *gin.Context) {
		h.post(c, "download")
	})
	r.POST("/download-torrent", func(c *gin.Context) {
		h.post(c, "download-torrent")
	})
	r.POST("/preview-image", func(c *gin.Context) {
		h.post(c, "preview-image")
	})
	r.POST("/stream-audio", func(c *gin.Context) {
		h.post(c, "stream-audio")
	})
	r.POST("/stream-video", func(c *gin.Context) {
		h.post(c, "stream-video")
	})
	r.PUT("/stream-video/subtitle", func(c *gin.Context) {
		a := TrackPutArgs{}
		if err := c.BindJSON(&a); err != nil {
			_ = c.Error(err)
			return
		}
		vsud := models.NewVideoStreamUserData(a.ResourceID, a.ItemID, nil)
		vsud.SubtitleID = a.ID
		if err := vsud.UpdateSessionData(c); err != nil {
			_ = c.Error(err)
		}
	})
	r.PUT("/stream-video/audio", func(c *gin.Context) {
		a := TrackPutArgs{}
		if err := c.BindJSON(&a); err != nil {
			_ = c.Error(err)
			return
		}
		vsud := models.NewVideoStreamUserData(a.ResourceID, a.ItemID, nil)
		vsud.AudioID = a.ID
		if err := vsud.UpdateSessionData(c); err != nil {
			_ = c.Error(err)
		}
	})
}

func (s *Handler) bindPostArgs(c *gin.Context) (*PostArgs, error) {
	rID, ok := c.GetPostFormArray("resource-id")
	if !ok {
		return nil, errors.Errorf("no resource id provided")
	}
	iID, ok := c.GetPostFormArray("item-id")
	if !ok {
		return nil, errors.Errorf("no item id provided")
	}

	purge := false
	if v, ok := c.GetPostForm("purge"); ok && v == "true" {
		purge = true
	}

	vsud := models.NewVideoStreamUserData(rID[0], iID[0], &models.StreamSettings{})
	vsud.FetchSessionData(c)

	return &PostArgs{
		ResourceID:          rID[0],
		ItemID:              iID[0],
		VideoStreamUserData: vsud,
		Purge:               purge,
	}, nil
}

func (s *Handler) post(c *gin.Context, action string) {
	var (
		d         PostData
		err       error
		args      *PostArgs
		actionJob *job.Job
	)
	postTpl := s.tb.Build("action/post")
	args, err = s.bindPostArgs(c)
	if err != nil {
		postTpl.HTML(
			http.StatusBadRequest,
			web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "wrong args provided")),
		)
		return
	}
	d.Args = args
	actionJob, err = s.jobs.Action(
		web.NewContext(c),
		args.ResourceID,
		args.ItemID,
		action,
		&models.StreamSettings{},
		args.Purge,
		args.VideoStreamUserData,
	)
	if err != nil {
		postTpl.HTML(
			http.StatusBadRequest,
			web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "failed to start downloading")),
		)
		return
	}
	d.Job = actionJob
	postTpl.HTML(http.StatusOK, web.NewContext(c).WithData(d))
}
