package auth

import (
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

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

	r.GET("/login", h.login)
	r.GET("/logout", h.logout)
	r.GET("/auth/verify", h.verify)
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
