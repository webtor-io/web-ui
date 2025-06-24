package support

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	proto "github.com/webtor-io/abuse-store/proto"
	"github.com/webtor-io/web-ui/services/abuse_store"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
	"google.golang.org/grpc/status"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	tb  template.Builder[*web.Context]
	asc *abuse_store.Client
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], asc *abuse_store.Client) {
	h := &Handler{
		tb:  tm.MustRegisterViews("support/*").WithLayout("main"),
		asc: asc,
	}
	r.GET("/support", h.process)
	r.POST("/support", h.process)
}

type Cause int

var hashRegexp = regexp.MustCompile("[0-9a-f]{40}")

var CauseTypes = map[int]string{
	-1: "Select cause...",
	0:  "Illegal content",
	1:  "Malware",
	2:  "Site error",
	3:  "Question",
}

type Form struct {
	Cause       Cause  `form:"cause"`
	Subject     string `form:"subject"`
	Description string `form:"description"`
	Infohash    string `form:"infohash"`
	Filename    string `form:"filename"`
	Email       string `form:"email"`
	Work        string `form:"work"`
}

type Data struct {
	Form       *Form
	CauseTypes map[int]string
}

func (s *Handler) bindForm(c *gin.Context) (*Form, error) {
	var err error
	cause := -1
	if c.PostForm("cause") != "" {
		cause, err = strconv.Atoi(c.PostForm("cause"))
		if err != nil {
			return nil, err
		}
	}
	if _, ok := CauseTypes[cause]; !ok {
		return nil, errors.Errorf("cause with id=%v not defined", c)
	}

	var form Form
	err = c.ShouldBind(&form)
	if err != nil {
		return nil, err
	}
	form.Cause = Cause(cause)
	return &form, nil
}

func (s *Handler) sendForm(c *gin.Context, form *Form) error {
	cl, err := s.asc.Get()
	if err != nil {
		return err
	}
	infohash := form.Infohash
	if form.Infohash != "" {
		infohash = strings.ToLower(hashRegexp.FindString(form.Infohash))
	}
	pr := &proto.PushRequest{
		NoticeId:    uuid.New().String(),
		Infohash:    infohash,
		Filename:    form.Filename,
		Work:        form.Work,
		StartedAt:   time.Now().Unix(),
		Email:       form.Email,
		Description: form.Description,
		Subject:     form.Subject,
		Cause:       proto.PushRequest_Cause(form.Cause),
		Source:      proto.PushRequest_FORM,
	}
	_, err = cl.Push(c.Request.Context(), pr)
	if err != nil {
		st, ok := status.FromError(err)
		if !ok {
			return errors.Wrap(err, "failed to send form")
		}
		return errors.New(st.Message())
	}
	return err
}

func (s *Handler) process(c *gin.Context) {
	tpl := s.tb.Build("support/form")
	form, err := s.bindForm(c)
	data := &Data{
		Form:       form,
		CauseTypes: CauseTypes,
	}
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(data).WithErr(errors.Wrap(err, "wrong args provided")))
		return
	}
	if c.Request.Method == "POST" {
		err = s.sendForm(c, form)
		if err != nil {
			tpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(data).WithErr(errors.Wrap(err, "failed to send form")))
			return
		}
		s.tb.Build("support/success").HTML(http.StatusOK, web.NewContext(c).WithData(data))
		return
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}
