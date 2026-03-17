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
	// Ensure RedirectWithError has a valid return URL (missing for magnet GET routes)
	if c.GetHeader("X-Return-Url") == "" {
		c.Request.Header.Set("X-Return-Url", "/")
	}

	args, err := s.bindArgs(c)
	if err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "wrong args provided"))
		return
	}

	loadJob, err := s.jobs.Load(web.NewContext(c), &scripts.LoadArgs{
		Query: args.Query,
		File:  args.File,
	})
	if err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "failed to load resource"))
		return
	}

	if !s.useDirectLinks {
		s.addResourceToSession(c, loadJob.ID)
	}

	if c.GetHeader("Accept") == "application/json" {
		c.JSON(http.StatusAccepted, gin.H{
			"job_log_url": fmt.Sprintf("/queue/%v/job/%v/log", loadJob.Queue, loadJob.ID),
		})
		return
	}

	s.tb.Build("index").HTML(http.StatusAccepted, web.NewContext(c).WithData(PostData{
		Job:         loadJob,
		Args:        args,
		Instruction: args.Instruction,
	}))
}
