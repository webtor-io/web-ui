package embed

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type GetData struct {
	CheckScript string
	CheckHash   string
	ID          string
}

func (s *Handler) get(c *gin.Context) {
	if !s.enabled {
		s.off(c)
		return
	}
	id := c.Query("id")
	code := uuid.New().String()
	h := sha1.New()
	h.Write([]byte(id + code))
	gd := GetData{
		CheckHash:   hex.EncodeToString(h.Sum(nil)),
		CheckScript: s.generateCheckScript(code, id),
		ID:          id,
	}
	s.tb.Build("embed/get").HTML(http.StatusOK, web.NewContext(c).WithData(gd))
}

func (s *Handler) generateCheckScript(code string, id string) string {
	return fmt.Sprintf(`
		var found = false;
		var scripts = document.getElementsByTagName('script');
			for (var i = scripts.length; i--;) {
				if (
					scripts[i].src.includes('https://cdn.jsdelivr.net/npm/@webtor/') ||
					scripts[i].src.includes('http://localhost:9009/')
				) {
					found = '%v';
				}
			}
		var f = window.frames['webtor-%v'];
		f.contentWindow.postMessage({id: '%v', name: 'check', data: found}, '*');
	`, code, id, id)
}

// off answers an iframe on another site when this instance does not serve
// embeds: a page that says so, instead of a play button that would only lead
// to the same message after a click. The frame's URL carries no language
// prefix and a cross-site frame does not send the SameSite=Lax lang cookie,
// so the browser's language decides; /embed is not a page to index.
func (s *Handler) off(c *gin.Context) {
	ctx := web.NewContext(c)
	if !i18n.Routed(c) {
		if l := i18n.BrowserLang(c); l != "" {
			ctx.Lang = l
		}
	}
	c.Header("Vary", "Accept-Language")
	s.tb.Build("embed/off").HTML(http.StatusOK, ctx)
}
