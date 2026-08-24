// Package notifications renders the in-app notification feed: the list page
// behind the navbar bell, and the action that clears its unread count.
//
// Two-level handler pattern (per CLAUDE.md): this file only unpacks the
// request and maps errors onto status codes; the actual reads and writes
// live in services/notification.
package notifications

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// feedLimit mirrors the retention cap in notification.go's cron path
// (notificationFeedCap) -- a user can never have more rows than that
// surviving pruning, so asking the store for more would be pointless.
const feedLimit = 100

type Handler struct {
	ns *notification.Service
	tb *template.BuilderWithLayout[*web.Context]
}

// Data is what the list view renders.
type Data struct {
	Notifications []models.Notification
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], ns *notification.Service) {
	h := &Handler{
		ns: ns,
		tb: tm.MustRegisterViews("notifications/*").WithLayout("main"),
	}
	gr := r.Group("/notifications")
	gr.GET("", auth.HasAuth, h.list)
	gr.POST("/read", auth.HasAuth, h.markRead)
}

func (h *Handler) list(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	list, err := h.ns.ListByUser(c.Request.Context(), user.ID, feedLimit)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	ctx := web.NewContext(c)
	h.tb.Build("notifications/get").HTML(http.StatusOK, ctx.WithData(&Data{Notifications: list}))
}

// markRead clears the unread count and sends the user back where they came
// from -- the bell is reachable from every page, so there is no single
// canonical "back".
func (h *Handler) markRead(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	if err := h.ns.MarkAllRead(c.Request.Context(), user.ID); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/notifications"))
}
