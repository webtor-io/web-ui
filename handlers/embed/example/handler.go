package example

import (
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
	"github.com/yargevad/filepathx"
	"net/http"
	"path/filepath"
	"strings"
)

type Handler struct {
	tb       template.Builder[*web.Context]
	examples Examples
}

type Example struct {
	Name string
}

type Examples []Example

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context]) {
	if !embed.Enabled(c) {
		return
	}
	examples := Examples{}
	g, err := filepathx.Glob("templates/views/embed/example/*")
	if err != nil {
		panic(err)
	}
	for _, f := range g {
		examples = append(examples, Example{
			Name: strings.TrimSuffix(filepath.Base(f), ".html"),
		})
	}

	h := &Handler{
		tb:       tm.MustRegisterViews("embed/example/*").WithLayout("embed/example"),
		examples: examples,
	}
	r.GET("/embed/example/:name", h.get)
	r.GET("/embed/example", h.exampleIndex)
}

type Data struct {
}

func (s *Handler) get(c *gin.Context) {
	s.tb.Build("embed/example/"+c.Param("name")).HTML(http.StatusOK, web.NewContext(c).WithData(&Data{}))
}

func (s *Handler) exampleIndex(c *gin.Context) {
	s.tb.Build("embed/example/index").HTML(http.StatusOK, web.NewContext(c).WithData(s.examples))
}
