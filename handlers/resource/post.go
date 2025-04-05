package resource

import (
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/handlers/job/script"
	"github.com/webtor-io/web-ui/services/web"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sv "github.com/webtor-io/web-ui/services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
)

type PostArgs struct {
	File        []byte
	Query       string
	Instruction string
	Claims      *api.Claims
}

func (s *Handler) bindArgs(c *gin.Context) (*PostArgs, error) {
	file, _ := c.FormFile("resource")
	instruction, _ := c.GetPostForm("instruction")
	query, _ := c.GetPostForm("resource")
	if query == "" && strings.HasPrefix(c.Request.URL.Path, "/magnet") {
		query = strings.TrimPrefix(c.Request.URL.Path, "/") + c.Request.URL.RawQuery
	}
	if query != "" {
		sha1 := sv.SHA1R.Find([]byte(query))
		if sha1 == nil {
			return &PostArgs{Query: query}, errors.Errorf("wrong resource provided query=%v", query)
		}
	}

	if file == nil && query == "" {
		return nil, errors.Errorf("no resource provided")
	}

	var fd []byte

	if file != nil {
		f, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func(f multipart.File) {
			_ = f.Close()
		}(f)
		fd, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
	}

	return &PostArgs{
		File:        fd,
		Query:       query,
		Claims:      api.GetClaimsFromContext(c),
		Instruction: instruction,
	}, nil
}

type PostData struct {
	Job         *job.Job
	Args        *PostArgs
	Instruction string
}

func (s *Handler) post(c *gin.Context) {
	c.Header("X-Robots-Tag", "noindex, nofollow")
	indexTpl := s.tb.Build("index")
	var (
		d       PostData
		err     error
		args    *PostArgs
		loadJob *job.Job
	)
	args, err = s.bindArgs(c)
	if err != nil {
		indexTpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "wrong args provided")))
	}
	d.Args = args
	d.Instruction = args.Instruction
	if err != nil {
		indexTpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "wrong args provided")))
		return
	}
	loadJob, err = s.jobs.Load(web.NewContext(c), &script.LoadArgs{
		Query: args.Query,
		File:  args.File,
	})
	if err != nil {
		indexTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "failed to load resource")))
		return
	}
	d.Job = loadJob
	indexTpl.HTML(http.StatusAccepted, web.NewContext(c).WithData(d))
}
