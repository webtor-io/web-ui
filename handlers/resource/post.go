package resource

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/jobs/scripts"
	sv "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
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
	Tool        *common.Tool
}

func (s *Handler) post(c *gin.Context) {
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
	loadJob, err = s.jobs.Load(web.NewContext(c), &scripts.LoadArgs{
		Query: args.Query,
		File:  args.File,
	})
	if err != nil {
		indexTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "failed to load resource")))
		return
	}

	d.Job = loadJob

	if !s.useDirectLinks {
		s.addResourceToSession(c, loadJob.ID)
	}

	if c.GetHeader("Accept") == "application/json" {
		c.JSON(http.StatusAccepted, gin.H{
			"job_log_url": fmt.Sprintf("/queue/%v/job/%v/log", loadJob.Queue, loadJob.ID),
		})
		return
	}

	indexTpl.HTML(http.StatusAccepted, web.NewContext(c).WithData(d))
}
