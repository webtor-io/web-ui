package action

import (
	"net/http"
	"net/url"
	"slices"
	"sort"

	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
)

// maxSelectedPaths and maxSelectedPathsEncodedLen mirror the rest-api
// bounds on the partial-archive selection (count, and percent-encoded byte
// length added to the signed download URL — edge proxies cap request lines
// at ~8k).
const (
	maxSelectedPaths           = 1024
	maxSelectedPathsEncodedLen = 6000
)

type PostArgs struct {
	ResourceID          string
	ItemID              string
	ApiClaims           *api.Claims
	UserClaims          *claims.Data
	Purge               bool
	ForceSlow           bool
	Debug               string
	ArchiveFormat       string
	SelectedPaths       []string
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
	jobs *j.Jobs
	tb   template.Builder[*web.Context]
	api  *api.Api
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], jobs *j.Jobs, apiSvc *api.Api) {
	h := &Handler{
		tb:   tm.MustRegisterViews("action/**/*").WithHelper(NewHelper()),
		jobs: jobs,
		api:  apiSvc,
	}
	r.POST("/download-file", func(c *gin.Context) {
		h.post(c, "download")
	})
	r.POST("/download-dir", func(c *gin.Context) {
		h.post(c, "download")
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

	forceSlow := false
	if v, ok := c.GetPostForm("force-slow"); ok && v == "true" {
		forceSlow = true
	}

	// Dev-only: lets the client force a specific error path via
	// `?debug=slow_download|no_peers` on the resource hash. Ignored in
	// release builds so the parameter can't be abused in prod.
	debug := ""
	if gin.Mode() != gin.ReleaseMode {
		if v, ok := c.GetPostForm("debug"); ok {
			debug = v
		}
	}

	// Only the directory-download forms post this field ("tar" is the UI
	// default: no per-file checksums → a resumed download never unpacks as
	// "corrupt"; "zip" stays available from the format dropdown). Gate on
	// field presence, not on the action name — both /download-file and
	// /download-dir run action "download", so the action can't tell them
	// apart. Forms without the field (streams, previews, single files)
	// keep it empty and rest-api applies its own default.
	archiveFormat := ""
	if v, ok := c.GetPostForm("archive-format"); ok {
		archiveFormat = "tar"
		if v == "zip" {
			archiveFormat = "zip"
		}
	}

	// Optional partial-archive selection: file/folder paths ticked in the
	// listing's select mode, one repeated "paths" field per path (select.js
	// keeps both TAR and ZIP forms in sync). Values stay verbatim — torrent
	// path components may legally contain whitespace — and get sorted so the
	// job cache key and everything downstream (rest-api URL, archiver ETag)
	// see the selection as a set. rest-api validates entries against the
	// torrent manifest.
	var selectedPaths []string
	if vs, ok := c.GetPostFormArray("paths"); ok {
		encodedLen := 0
		for _, p := range vs {
			if p == "" {
				continue
			}
			encodedLen += len(url.QueryEscape(p)) + len("&paths=")
			selectedPaths = append(selectedPaths, p)
		}
		if len(selectedPaths) > maxSelectedPaths {
			return nil, errors.Errorf("too many paths selected (max %d)", maxSelectedPaths)
		}
		if encodedLen > maxSelectedPathsEncodedLen {
			return nil, errors.Errorf("selected paths too long (max %d encoded bytes)", maxSelectedPathsEncodedLen)
		}
		sort.Strings(selectedPaths)
		selectedPaths = slices.Compact(selectedPaths)
	}

	vsud := models.NewVideoStreamUserData(rID[0], iID[0], &models.StreamSettings{})
	vsud.FetchSessionData(c)

	return &PostArgs{
		ResourceID:          rID[0],
		ItemID:              iID[0],
		VideoStreamUserData: vsud,
		Purge:               purge,
		ForceSlow:           forceSlow,
		Debug:               debug,
		ArchiveFormat:       archiveFormat,
		SelectedPaths:       selectedPaths,
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
		args.ForceSlow,
		args.Debug,
		args.ArchiveFormat,
		args.SelectedPaths,
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
