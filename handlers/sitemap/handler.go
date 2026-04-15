package sitemap

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/handlers/common"
	svc "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
)

type Link struct {
	XMLName xml.Name `xml:"xhtml:link"`
	Rel     string   `xml:"rel,attr"`
	Hreflang string  `xml:"hreflang,attr"`
	Href    string   `xml:"href,attr"`
}

type URL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
	Links      []Link `xml:"xhtml:link,omitempty"`
}

type URLSet struct {
	XMLName    xml.Name `xml:"urlset"`
	Xmlns      string   `xml:"xmlns,attr"`
	XmlnsXhtml string   `xml:"xmlns:xhtml,attr"`
	URLs       []URL    `xml:"url"`
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

// langLinks builds hreflang alternate links for a given path.
func (h *Handler) langLinks(path string) []Link {
	links := make([]Link, 0, len(i18n.SupportedLangs)+1)
	for _, lang := range i18n.SupportedLangs {
		href := h.baseURL
		if lang != i18n.DefaultLang {
			href += "/" + lang
		}
		href += path
		links = append(links, Link{Rel: "alternate", Hreflang: lang, Href: href})
	}
	links = append(links, Link{Rel: "alternate", Hreflang: "x-default", Href: h.baseURL + path})
	return links
}

func (h *Handler) sitemap(c *gin.Context) {
	now := time.Now().Format("2006-01-02")

	urls := []URL{
		{
			Loc:        h.baseURL + "/",
			LastMod:    now,
			ChangeFreq: "daily",
			Priority:   "1.0",
			Links:      h.langLinks("/"),
		},
	}

	urls = append(urls, URL{
		Loc:        h.baseURL + "/about",
		LastMod:    now,
		ChangeFreq: "monthly",
		Priority:   "0.7",
		Links:      h.langLinks("/about"),
	})

	for _, tool := range common.Tools {
		urls = append(urls, URL{
			Loc:        h.baseURL + "/" + tool.Url,
			LastMod:    now,
			ChangeFreq: "weekly",
			Priority:   "0.8",
			Links:      h.langLinks("/" + tool.Url),
		})
	}

	urlSet := URLSet{
		Xmlns:      "http://www.sitemaps.org/schemas/sitemap/0.9",
		XmlnsXhtml: "http://www.w3.org/1999/xhtml",
		URLs:       urls,
	}

	c.Header("Content-Type", "application/xml")
	c.XML(http.StatusOK, urlSet)
}
