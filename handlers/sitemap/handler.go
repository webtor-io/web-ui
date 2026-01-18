package sitemap

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/handlers/common"
	svc "github.com/webtor-io/web-ui/services/common"
)

type URL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	URLs    []URL    `xml:"url"`
}

type Handler struct {
	baseURL string
}

func RegisterHandler(c *cli.Context, r *gin.Engine) {
	h := &Handler{
		baseURL: c.String(svc.DomainFlag),
	}
	r.GET("/sitemap.xml", h.sitemap)
}

func (h *Handler) sitemap(c *gin.Context) {
	now := time.Now().Format("2006-01-02")

	urls := []URL{
		{
			Loc:        h.baseURL + "/",
			LastMod:    now,
			ChangeFreq: "daily",
			Priority:   "1.0",
		},
	}

	for _, tool := range common.Tools {
		urls = append(urls, URL{
			Loc:        h.baseURL + "/" + tool.Url,
			LastMod:    now,
			ChangeFreq: "weekly",
			Priority:   "0.8",
		})
	}

	urlSet := URLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	c.Header("Content-Type", "application/xml")
	c.XML(http.StatusOK, urlSet)
}
