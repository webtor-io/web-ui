package auth

import (
	"net/http"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/template"
)

type LoginData struct {
	Instruction string
}

type LogoutData struct{}

type VerifyData struct {
	PreAuthSessionId string
}

type Handler struct {
	tb template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context]) {
	h := &Handler{
		tb: tm.MustRegisterViews("auth/*").WithLayout("main"),
	}

	r.Use(func(c *gin.Context) {
		u := auth.GetUserFromContext(c)
		if u != nil && u.Expired {
			h.refresh(c)
			c.Abort()
			return
		}
	})

	r.GET("/login", h.login)
	r.GET("/refresh", h.refresh)
	r.GET("/logout", h.logout)
	r.GET("/auth/verify", h.verify)
	r.GET("/auth/callback/google", h.callback)
	r.GET("/auth/callback/patreon", h.callback)
}

func (s *Handler) refresh(c *gin.Context) {
	s.tb.Build("auth/refresh").HTML(http.StatusOK, web.NewContext(c))
}

func (s *Handler) login(c *gin.Context) {
	instruction := "default"
	if c.Query("from") != "" {
		instruction = c.Query("from")
	}
	ld := LoginData{
		Instruction: instruction,
	}
	s.tb.Build("auth/login").HTML(http.StatusOK, web.NewContext(c).WithData(ld))
}

func (s *Handler) logout(c *gin.Context) {
	s.tb.Build("auth/logout").HTML(http.StatusOK, web.NewContext(c).WithData(LogoutData{}))
}

func (s *Handler) verify(c *gin.Context) {
	s.tb.Build("auth/verify").HTML(http.StatusOK, web.NewContext(c).WithData(&VerifyData{
		PreAuthSessionId: c.Query("preAuthSessionId"),
	}))
}

func (s *Handler) callback(c *gin.Context) {
	s.tb.Build("auth/callback").HTML(http.StatusOK, web.NewContext(c))
}
