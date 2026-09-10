package action

import (
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/services/auth"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/turnstile"

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
	jobs     *j.Jobs
	tb       template.Builder[*web.Context]
	api      *api.Api
	verifier ActionVerifier
}

// ActionVerifier checks the Turnstile token a job start carries. Satisfied
// by *turnstile.Service; a nil verifier means the widget is not configured
// and nothing is checked.
type ActionVerifier interface {
	Validate(token string, remoteIP string) error
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], jobs *j.Jobs, apiSvc *api.Api, verifier ActionVerifier) {
	h := &Handler{
		tb:       tm.MustRegisterViews("action/**/*").WithHelper(NewHelper()),
		jobs:     jobs,
		api:      apiSvc,
		verifier: verifier,
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
	// Anonymous job starts carry a Turnstile token from the invisible
	// widget on the page (assets/src/js/lib/turnstileAction.js). A bot
	// that cannot run the widget gets the card and no seeder is touched;
	// a signed-in person is not asked. Fail closed: a missing token is a
	// refusal, otherwise skipping the script would be the bypass.
	if err := s.verifyAction(c); err != nil {
		logRefusal(c, action, err)
		postTpl.HTML(
			http.StatusBadRequest,
			web.NewContext(c).WithData(d).WithErr(web.NewUserError("error.turnstile_failed", err)),
		)
		return
	}
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

// verifyAction runs the Turnstile check for anonymous requests when a
// verifier is configured. The client's address is what Cloudflare saw
// (CF-Connecting-IP), not the edge's; without the header siteverify simply
// skips the address match.
func (s *Handler) verifyAction(c *gin.Context) error {
	if s.verifier == nil || isNilVerifier(s.verifier) {
		return nil
	}
	// GetUserFromContext hands back an empty User for a guest; signed in
	// means an account id.
	if u := auth.GetUserFromContext(c); u != nil && u.ID != uuid.Nil {
		return nil
	}
	return s.verifier.Validate(c.PostForm("cf-turnstile-response"), c.GetHeader("CF-Connecting-IP"))
}

// logRefusal leaves one warning per refused start with what is needed to
// tell the cases apart: siteverify's codes (missing token = a client that
// never ran the widget; timeout-or-duplicate = a token used twice or too
// late), the country Cloudflare saw, the User-Agent and the referer. No
// token contents.
func logRefusal(c *gin.Context, action string, err error) {
	codes := "unknown"
	var ve *turnstile.VerifyError
	if errors.As(err, &ve) && len(ve.Codes) > 0 {
		codes = strings.Join(ve.Codes, ",")
	}
	ua := c.Request.UserAgent()
	if len(ua) > 120 {
		ua = ua[:120]
	}
	// reason is what the client says happened when it sent no token
	// (turnstileAction.js: no-script, silent-timeout, interactive-timeout,
	// widget-error…, with the elapsed ms); "absent" means the request did
	// not carry the field at all — no script ran the interception.
	reason := c.PostForm("cf-turnstile-reason")
	if _, ok := c.GetPostForm("cf-turnstile-response"); !ok {
		reason = "absent"
	}
	log.WithFields(log.Fields{
		"action":    action,
		"codes":     codes,
		"reason":    reason,
		"token_len": len(c.PostForm("cf-turnstile-response")),
		"country":   c.GetHeader("CF-IPCountry"),
		"ua":        ua,
		"referer":   c.Request.Referer(),
	}).Warn("turnstile refused job start")
}

// isNilVerifier catches a typed nil (*turnstile.Service)(nil) stored in the
// interface, which is what the constructor returns when unconfigured.
func isNilVerifier(v ActionVerifier) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}
