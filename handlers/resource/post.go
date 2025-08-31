package resource

import (
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/handlers/job/script"
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
}

func (s *Handler) post(c *gin.Context) {
	// Enable search engine blocking for POST as well
	c.Header("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
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

	// Track resource in session for guest access (WEB-15 requirement)
	//s.addResourceToSession(c, loadJob.ID)

	indexTpl.HTML(http.StatusAccepted, web.NewContext(c).WithData(d))
}

// addResourceToSession adds a resource ID to the current session for guest access tracking
func (s *Handler) addResourceToSession(c *gin.Context, resourceID string) {
	session := sessions.Default(c)

	// Get existing resources from session
	sessionResources := session.Get("resources")
	var resources []string

	if sessionResources != nil {
		if existingResources, ok := sessionResources.([]string); ok {
			resources = existingResources
		}
	}

	// Check if resource is already in session
	for _, existing := range resources {
		if existing == resourceID {
			return // Already tracked
		}
	}

	// Add new resource to session (limit to 50 resources to prevent session bloat)
	resources = append(resources, resourceID)
	if len(resources) > 50 {
		resources = resources[len(resources)-50:] // Keep only last 50
	}

	session.Set("resources", resources)
	_ = session.Save()
}
