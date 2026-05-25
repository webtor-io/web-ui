package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/data_export"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/stremio"
	ua "github.com/webtor-io/web-ui/services/url_alias"
	usettings "github.com/webtor-io/web-ui/services/user_settings"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

// BackendTypeInfo represents information about a streaming backend type
type BackendTypeInfo struct {
	Type        string
	DisplayName string
}

type Data struct {
	StremioAddonURL       string
	WebDAVURL             string
	EmbedDomains          []models.EmbedDomain
	AddonUrls             []models.StremioAddonUrl
	StremioSettings       *models.StremioSettingsData
	StreamingBackends     []*models.StreamingBackend
	AvailableBackendTypes []BackendTypeInfo
	Is4KAvailable         bool
	MinBitrateFor4KMbps   int64
	VaultStats            *vault.UserStats
	UserSettings          *models.UserSettings
	ErrKey                string
	DisableWebDAV         bool
	DisableEmbed          bool
}

type Handler struct {
	tb            template.Builder[*web.Context]
	ual           *ua.UrlAlias
	at            *at.AccessToken
	pg            *cs.PG
	claims        *claims.Claims
	vault         *vault.Vault
	userSettings  *usettings.Service
	disableWebDAV bool
	disableEmbed  bool
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], at *at.AccessToken, ual *ua.UrlAlias, pg *cs.PG, cl *claims.Claims, v *vault.Vault, us *usettings.Service) {
	h := &Handler{
		tb:            tm.MustRegisterViews("profile/*").WithLayout("main"),
		at:            at,
		ual:           ual,
		pg:            pg,
		claims:        cl,
		vault:         v,
		userSettings:  us,
		disableWebDAV: c.Bool(common.DisableWebDAVFlag),
		disableEmbed:  c.Bool(common.DisableEmbedFlag),
	}
	r.GET("/profile", h.get)
	gr := r.Group("/profile")
	gr.Use(auth.HasAuth)
	gr.POST("/delete", h.delete)
	gr.GET("/export", h.export)
	gr.POST("/settings", h.updateSettings)
}

// getAvailableBackendTypes returns the list of available streaming backend types
func getAvailableBackendTypes() []BackendTypeInfo {
	return []BackendTypeInfo{
		{Type: string(models.StreamingBackendTypeRealDebrid), DisplayName: "Real-Debrid"},
		{Type: string(models.StreamingBackendTypeTorbox), DisplayName: "Torbox"},
	}
}

func (s *Handler) getStremioAddonURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "stremio")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/stremio/", common.AccessTokenParamName, at.Token)

	al, err := s.ual.Get(c.Request.Context(), url, false)
	if err != nil {
		return "", err
	}
	return al + "/manifest.json", nil

}

func (s *Handler) getWebDAVURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "webdav")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/webdav/fs/", common.AccessTokenParamName, at.Token)

	al, err := s.ual.Get(c.Request.Context(), url, true)
	if err != nil {
		return "", err
	}
	return al + "/webdav/", nil
}

func deleteUser(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	return models.DeleteUser(ctx, db, userID)
}

func (s *Handler) delete(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if err := deleteUser(c.Request.Context(), db, u.ID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, "/logout")
}

// buildExport is the pure-data level: load the full user record and assemble
// the export payload. Returns nil when the authenticated user_id no longer
// exists in the DB (e.g. mid-deletion race).
func buildExport(ctx context.Context, db *pg.DB, userID uuid.UUID) (*data_export.Export, error) {
	u, err := models.GetUserByID(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	return data_export.Build(ctx, db, u)
}

func (s *Handler) export(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	exp, err := buildExport(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build data export"))
		return
	}
	if exp == nil {
		c.Redirect(http.StatusFound, "/logout")
		return
	}
	filename := fmt.Sprintf("webtor-data-export-%s.json", time.Now().UTC().Format("2006-01-02"))
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")
	enc := json.NewEncoder(c.Writer)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exp); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to write data export"))
		return
	}
}

func (s *Handler) get(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Redirect(http.StatusTemporaryRedirect, "/login")
		return
	}
	stremioURL, err := s.getStremioAddonURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get stremio addon url"))
		return
	}
	webdavURL, err := s.getWebDAVURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get webdav url"))
		return
	}

	// Get user domains
	db := s.pg.Get()
	domains, err := models.GetUserDomains(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user domains"))
		return
	}

	// Get user addon URLs
	addonUrls, err := models.GetAllUserStremioAddonUrls(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user addon urls"))
		return
	}

	// Get Stremio settings. When the user has never saved settings, prefill
	// the preferred language with the current UI language so the dropdown
	// shows a sensible default — saving the form locks it in.
	existingSS, err := models.GetUserStremioSettings(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get stremio settings"))
		return
	}
	var ss *models.StremioSettingsData
	if existingSS == nil {
		ss = models.GetDefaultStremioSettings()
		if l := stremio.LanguageByCode(i18n.GetLang(c)); l != nil {
			ss.PreferredLanguage = l.Code
		}
	} else {
		ss = existingSS.Settings
	}

	// Get user streaming backends
	streamingBackends, err := models.GetUserStreamingBackends(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user streaming backends"))
		return
	}

	// Get vault statistics if vault service is available
	var vaultStats *vault.UserStats
	if s.vault != nil {
		vaultStats, _, err = s.vault.GetUserStats(c.Request.Context(), u)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get vault user stats"))
			return
		}
	}

	// Per-user preferences (adult-content visibility, etc). Missing row
	// falls through to Default() — the toggle renders unchecked.
	userSettings, err := s.userSettings.Get(c.Request.Context(), u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user settings"))
		return
	}

	s.tb.Build("profile/get").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		StremioAddonURL:       stremioURL,
		WebDAVURL:             webdavURL,
		EmbedDomains:          domains,
		AddonUrls:             addonUrls,
		StremioSettings:       ss,
		StreamingBackends:     streamingBackends,
		AvailableBackendTypes: getAvailableBackendTypes(),
		VaultStats:            vaultStats,
		UserSettings:          userSettings,
		ErrKey:                c.Query("err"),
		DisableWebDAV:         s.disableWebDAV,
		DisableEmbed:          s.disableEmbed,
	}))
}

// updateSettings persists the toggles from the per-user settings
// section of the profile page. Form is data-async so the response
// re-renders the section in place; redirect with X-Return-Url
// preserves the user's scroll position.
func (s *Handler) updateSettings(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	us := &models.UserSettings{
		UserID:    u.ID,
		ShowAdult: c.PostForm("show_adult") == "true",
	}
	if err := s.userSettings.Set(c.Request.Context(), us); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	ret := c.GetHeader("X-Return-Url")
	if ret == "" {
		ret = "/profile"
	}
	c.Redirect(http.StatusFound, ret)
}
