// Package device serves the human half of the device authorization flow:
// the page where a signed-in user types (or arrives with) the short code a
// CLI or TV showed, sees what is being granted, and confirms. The machine
// half — code issuance and polling — lives in handlers/api.
package device

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/template"
	web "github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb template.Builder[*web.Context]
	pg *cs.PG
	// limiter bounds confirmation attempts per user: the user code is short,
	// and this form is the only place it can be guessed against.
	limiter *libapi.RateLimiter
}

// Data drives the single view. State is one of:
// "input" (ask for a code), "confirm" (code resolved, show what it grants),
// "done" (key issued), "invalid" (unknown/expired code).
type Data struct {
	State      string
	Code       string
	DeviceName string
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		tb:      tm.MustRegisterViews("device/*").WithLayout("main"),
		pg:      pg,
		limiter: libapi.NewRateLimiterWith(0.1, 5),
	}
	r.GET(libapi.DevicePath, h.get)
	gr := r.Group(libapi.DevicePath)
	gr.Use(auth.HasAuth)
	gr.Use(claims.IsPaid)
	gr.POST("/confirm", h.confirm)

	// Revoke is deliberately NOT paid-gated: a lapsed account must still be
	// able to cut off a device it no longer trusts.
	rr := r.Group(libapi.DevicePath)
	rr.Use(auth.HasAuth)
	rr.POST("/revoke", h.revoke)
}

// revoke deletes one device key from the profile's device list. The prefix
// check is the whole security story: without it this form could delete the
// account's "api", "webdav" or Stremio tokens by name.
func (s *Handler) revoke(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if u == nil || !u.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}
	name := c.PostForm("name")
	if !strings.HasPrefix(name, libapi.DeviceTokenPrefix) {
		c.Status(http.StatusBadRequest)
		return
	}
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("database is not available"))
		return
	}
	if _, err := models.DeleteAccessToken(c.Request.Context(), db, u.ID, name); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to revoke device key"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.deviceRevoked")
}

func (s *Handler) get(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// Same shape as the profile page: preserve the deep link (the code is
		// in the query) so sign-in lands back on the confirmation.
		lang := i18n.GetLang(c)
		returnURL := i18n.LangPath(lang, c.Request.URL.Path)
		if rq := c.Request.URL.RawQuery; rq != "" {
			returnURL += "?" + rq
		}
		v := url.Values{"return-url": []string{returnURL}}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}

	ctx := web.NewContext(c)
	if c.Query("done") != "" {
		s.tb.Build("device/get").HTML(http.StatusOK, ctx.WithData(&Data{State: "done"}))
		return
	}
	code := libapi.NormalizeUserCode(c.Query("code"))
	if code == "" {
		s.tb.Build("device/get").HTML(http.StatusOK, ctx.WithData(&Data{State: "input"}))
		return
	}
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("database is not available"))
		return
	}
	da, err := models.GetDeviceAuthByUserCode(c.Request.Context(), db, code)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to look up device code"))
		return
	}
	if da == nil {
		s.tb.Build("device/get").HTML(http.StatusOK, ctx.WithData(&Data{State: "invalid", Code: code}))
		return
	}
	s.tb.Build("device/get").HTML(http.StatusOK, ctx.WithData(&Data{
		State:      "confirm",
		Code:       da.UserCode,
		DeviceName: da.DeviceName,
	}))
}

func (s *Handler) confirm(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if u == nil || !u.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}
	lang := i18n.GetLang(c)
	// Guessing budget: the code space is small on purpose, this counter is
	// what makes that acceptable.
	if s.limiter != nil {
		if _, ok := s.limiter.Take("confirm:" + u.ID.String()); !ok {
			c.Redirect(http.StatusFound, i18n.LangPath(lang, libapi.DevicePath))
			return
		}
	}
	code := libapi.NormalizeUserCode(c.PostForm("code"))
	db := s.pg.Get()
	if code == "" || db == nil {
		c.Redirect(http.StatusFound, i18n.LangPath(lang, libapi.DevicePath))
		return
	}
	da, err := models.GetDeviceAuthByUserCode(c.Request.Context(), db, code)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to look up device code"))
		return
	}
	if da == nil {
		c.Redirect(http.StatusFound, i18n.LangPath(lang, libapi.DevicePath)+"?code="+url.QueryEscape(code))
		return
	}

	// One access_token row per device. The user code makes the name unique,
	// so MakeAccessToken's keep-on-conflict semantics never hand two devices
	// the same key; the label part is what the profile's device list shows.
	label := da.DeviceName
	if label == "" {
		label = "device"
	}
	name := libapi.DeviceTokenPrefix + label + " · " + da.UserCode
	token, err := models.MakeAccessToken(c.Request.Context(), db, u.ID, name,
		[]string{libapi.ScopeRead, libapi.ScopeWrite})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to issue device key"))
		return
	}
	ok, err := models.ConfirmDeviceAuth(c.Request.Context(), db, da.DeviceCode, u.ID, token.Token)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to confirm device"))
		return
	}
	if !ok {
		// Raced with expiry or another confirmation; the parked key row is
		// orphaned but harmless — the device never learns it, and the user
		// can revoke it from the profile like any other.
		c.Redirect(http.StatusFound, i18n.LangPath(lang, libapi.DevicePath)+"?code="+url.QueryEscape(code))
		return
	}
	c.Redirect(http.StatusFound, i18n.LangPath(lang, libapi.DevicePath)+"?done=1")
}
