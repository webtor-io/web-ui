package user_subtitle

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/template"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	uploadTimeout = 30 * time.Second
	deleteTimeout = 30 * time.Second
	fileTimeout   = 30 * time.Second
)

type Handler struct {
	svc  *us.Service
	sapi *api.Api
	tb   template.Builder[*web.Context]
}

// RegisterHandler mounts the /user-subtitle routes. When the service is
// disabled (no bucket configured) nothing is registered and the URLs 404 —
// the UI checks Enabled() and hides the "My Subtitles" tab entirely.
func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], svc *us.Service, sapi *api.Api) {
	if !svc.Enabled() {
		return
	}
	h := &Handler{
		svc:  svc,
		sapi: sapi,
		tb: tm.MustRegisterViews("user_subtitle/*").
			WithLayout("main"),
	}

	// Public file endpoint: torrent-http-proxy fetches this via /ext/ when
	// wrapping the URL for SRT → VTT conversion, so it must stay outside
	// the auth group. Hash addressing keeps it unguessable.
	r.GET("/user-subtitle/file/:hash/*name", h.file)

	gr := r.Group("/user-subtitle")
	gr.Use(auth.HasAuth)
	gr.POST("", h.upload)
	gr.POST("/delete/:id", h.delete)
}

func (s *Handler) upload(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	resourceID := strings.TrimSpace(c.PostForm("resource_id"))
	path := c.PostForm("path")
	if resourceID == "" || path == "" {
		s.respondError(c, user.ID, resourceID, path, errors.New("no resource provided"))
		return
	}

	file, err := c.FormFile("file")
	if err != nil || file == nil {
		s.respondError(c, user.ID, resourceID, path, web.NewUserError("error.user_subtitle.no_file", errors.Wrap(err, "no file provided")))
		return
	}

	if file.Size > us.MaxUploadSize {
		s.respondError(c, user.ID, resourceID, path, web.NewUserError("error.user_subtitle.too_large", errors.New("file too large")))
		return
	}

	data, err := readMultipart(file)
	if err != nil {
		s.respondError(c, user.ID, resourceID, path, web.NewUserError("error.user_subtitle.read_failed", err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), uploadTimeout)
	defer cancel()

	if _, err := s.svc.Upload(ctx, user.ID, resourceID, path, file.Filename, data); err != nil {
		s.respondError(c, user.ID, resourceID, path, translateUploadError(err))
		return
	}

	s.respondSuccess(c, user.ID, resourceID, path, "toast.user_subtitle.uploaded")
}

func (s *Handler) delete(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "wrong id"))
		return
	}

	// Load the binding before deletion so async re-renders can include the
	// same (resource_id, path) context the form belonged to.
	loadCtx, loadCancel := context.WithTimeout(c.Request.Context(), deleteTimeout)
	defer loadCancel()

	var resourceID, path string
	if sub, err := s.svc.Get(loadCtx, user.ID, id); err == nil && sub != nil {
		resourceID = sub.ResourceID
		path = sub.Path
	}

	delCtx, delCancel := context.WithTimeout(c.Request.Context(), deleteTimeout)
	defer delCancel()

	if err := s.svc.Delete(delCtx, user.ID, id); err != nil {
		if errors.Is(err, us.ErrNotFound) {
			s.respondError(c, user.ID, resourceID, path, web.NewUserError("error.not_found", err))
			return
		}
		s.respondError(c, user.ID, resourceID, path, err)
		return
	}

	s.respondSuccess(c, user.ID, resourceID, path, "toast.user_subtitle.deleted")
}

// file streams the raw blob. The :name suffix is decorative (so the player
// and proxies preserve a sensible filename) — only :hash is used for lookup.
func (s *Handler) file(c *gin.Context) {
	hash := c.Param("hash")
	if hash == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), fileTimeout)
	defer cancel()

	r, size, err := s.svc.GetFile(ctx, hash)
	if err != nil {
		if errors.Is(err, us.ErrNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = r.Close() }()

	name := strings.TrimPrefix(c.Param("name"), "/")
	c.Header("Content-Type", contentTypeFromName(name))
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	if size > 0 {
		c.Header("Content-Length", strconv.FormatInt(size, 10))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, r)
}

// respondSuccess renders the async view on XHR requests (so the "My
// Subtitles" panel swaps in place) and falls back to the classic redirect
// for non-async form submits.
func (s *Handler) respondSuccess(c *gin.Context, userID uuid.UUID, resourceID, path, toastKey string) {
	if isAsync(c) {
		s.renderView(c, userID, resourceID, path, c.PostForm("ei_url"), "")
		return
	}
	web.RedirectWithSuccessAndMessage(c, toastKey)
}

func (s *Handler) respondError(c *gin.Context, userID uuid.UUID, resourceID, path string, err error) {
	if isAsync(c) && resourceID != "" && path != "" {
		s.renderView(c, userID, resourceID, path, c.PostForm("ei_url"), web.ClassifyError(err))
		return
	}
	web.RedirectWithError(c, err)
}

// renderView builds the async response: load the current list for the file,
// feed it into the user_subtitles_view partial via tb.Build().HTML(). The
// template manager sees X-Requested-With + X-Layout (set by the client from
// the target element's data-async-layout) and wraps our view name with that
// layout body, so the target ends up populated with a freshly-rendered list.
//
// eiURL is the ExportItem "stream" URL the client carries back from the
// initial render (hidden form field). We use it as the base when wrapping
// each subtitle through /ext/~vtt/ so the proxy's embedded auth (whether
// in the subdomain, path, or query) is preserved verbatim.
func (s *Handler) renderView(c *gin.Context, userID uuid.UUID, resourceID, path, eiURL, errKey string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), deleteTimeout)
	defer cancel()

	list, listErr := s.svc.List(ctx, userID, resourceID, path)
	if listErr != nil && errKey == "" {
		errKey = web.ClassifyError(listErr)
	}

	ei := ra.ExportItem{URL: eiURL}
	tracks := make([]models.UserSubtitleTrack, 0, len(list))
	for _, sub := range list {
		var wrapped string
		if eiURL != "" && s.sapi != nil {
			wrapped = s.sapi.AttachExternalSubtitle(ei, s.svc.PublicURL(sub.Hash, sub.OriginalName))
		}
		tracks = append(tracks, models.UserSubtitleTrack{
			ID:           us.TrackID(sub.UserSubtitleID),
			Label:        sub.OriginalName,
			OriginalName: sub.OriginalName,
			Format:       sub.Format,
			Size:         sub.Size,
			Src:          wrapped,
			DeleteURL:    us.DeleteURL(sub.UserSubtitleID),
		})
	}
	data := &models.UserSubtitleView{
		ResourceID:    resourceID,
		Path:          path,
		EIURL:         eiURL,
		UserSubtitles: tracks,
		ErrKey:        errKey,
	}
	s.tb.Build("user_subtitle/view").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

func isAsync(c *gin.Context) bool {
	return c.GetHeader("X-Requested-With") == "XMLHttpRequest" && c.GetHeader("X-Layout") != ""
}

func readMultipart(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, errors.Wrap(err, "failed to open uploaded file")
	}
	defer func() { _ = f.Close() }()
	// Cap the read at MaxUploadSize+1 so an oversized file is rejected
	// here as well (fh.Size already gatekeeps, but belt-and-braces).
	return io.ReadAll(io.LimitReader(f, us.MaxUploadSize+1))
}

func translateUploadError(err error) error {
	switch {
	case errors.Is(err, us.ErrTooLarge):
		return web.NewUserError("error.user_subtitle.too_large", err)
	case errors.Is(err, us.ErrUnsupportedFormat):
		return web.NewUserError("error.user_subtitle.unsupported_format", err)
	case errors.Is(err, us.ErrLimitReached):
		return web.NewUserError("error.user_subtitle.limit_reached", err)
	case errors.Is(err, us.ErrEmptyFile):
		return web.NewUserError("error.user_subtitle.empty_file", err)
	case errors.Is(err, us.ErrNotConfigured):
		return web.NewUserError("error.service_unavailable", err)
	}
	return err
}

func contentTypeFromName(name string) string {
	return us.ContentTypeFor(strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."))
}
