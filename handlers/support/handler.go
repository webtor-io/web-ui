package support

import (
	"encoding/base32"
	"encoding/hex"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	proto "github.com/webtor-io/abuse-store/proto"
	"github.com/webtor-io/web-ui/services/abuse_store"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/turnstile"
	"github.com/webtor-io/web-ui/services/web"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	tb  template.Builder[*web.Context]
	asc *abuse_store.Client
	ts  *turnstile.Service
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], asc *abuse_store.Client, ts *turnstile.Service) {
	h := &Handler{
		tb:  tm.MustRegisterViews("support/*").WithLayout("main"),
		asc: asc,
		ts:  ts,
	}
	r.GET("/support", h.process)
	r.POST("/support", h.process)
}

type Cause int

var (
	// hashTokenRe walks the text as maximal runs of ASCII letters and digits,
	// so a hash counts only when bounded by something else or the text's edge:
	// a 64-hex v2 hash or a longer hex run is one token and yields nothing. A
	// "btih:" prefix is captured with its value for base32 magnets.
	hashTokenRe     = regexp.MustCompile(`btih:([0-9a-z]+)|[0-9a-z]+`)
	infohashRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	percentEscapeRe = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
)

const maxNoticeHashes = 100

// extractInfohashes pulls every v1 infohash out of whatever the reporter
// pasted: bare hashes, magnet links (hex or base32), webtor URLs, a list in
// the description. Lower-cased, first-seen order, no repeats, at most
// maxNoticeHashes.
//
// The text is percent-decoded first (up to 3 times, for double-encoded
// links): the old unanchored match took the first 40 hex characters, so
// "btih%3A<hash>" yielded a hash shifted by the "3a" of the escape.
func extractInfohashes(in string) []string {
	for range 3 {
		d := percentEscapeRe.ReplaceAllStringFunc(in, func(e string) string {
			b, _ := strconv.ParseUint(e[1:], 16, 8)
			return string([]byte{byte(b)})
		})
		if d == in {
			break
		}
		in = d
	}
	var hashes []string
	for _, m := range hashTokenRe.FindAllStringSubmatch(strings.ToLower(in), -1) {
		h := m[0]
		if v := m[1]; v != "" {
			h = v
			// 32 characters is the base32 form. A 40-character value stays
			// hex even when every digit is also a base32 letter.
			if len(v) == 32 {
				if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(v)); err == nil {
					h = hex.EncodeToString(b)
				}
			}
		}
		if !infohashRe.MatchString(h) || slices.Contains(hashes, h) {
			continue
		}
		hashes = append(hashes, h)
		if len(hashes) == maxNoticeHashes {
			break
		}
	}
	return hashes
}

var CauseTypes = map[int]string{
	-1: "support.selectCause",
	0:  "support.illegalContent",
	1:  "support.malware",
	2:  "support.siteError",
	3:  "support.question",
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

// needsInfohash: complaints about content, malware or a site error are about
// a specific torrent and cannot be acted on without knowing which. Questions
// are not. Enforced server-side — the form's JS-only `required` used to be the
// only guard, and tickets arrived with the field empty.
func needsInfohash(cause Cause) bool {
	return cause >= 0 && cause <= 2
}

func (s *Handler) bindForm(c *gin.Context) (*Form, error) {
	var err error
	cause := -1
	// GET prefills from the query so a "report a problem" link on a torrent
	// page can hand over the cause and infohash; POST reads the form.
	rawCause := c.PostForm("cause")
	if rawCause == "" && c.Request.Method == http.MethodGet {
		rawCause = c.Query("cause")
	}
	if rawCause != "" {
		cause, err = strconv.Atoi(rawCause)
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
	if c.Request.Method == http.MethodGet {
		form.Infohash = c.Query("infohash")
		form.Filename = c.Query("filename")
	}
	return &form, nil
}

func (s *Handler) sendForm(c *gin.Context, form *Form) error {
	cl, err := s.asc.Get()
	if err != nil {
		return err
	}
	// One notice bans every hash it names. The infohash field goes first, so
	// its first hash is the primary, else the description's; the newline keeps
	// the two fields from running into one token.
	hashes := extractInfohashes(form.Infohash + "\n" + form.Description)
	var primary string
	if len(hashes) > 0 {
		primary, hashes = hashes[0], hashes[1:]
	}
	pr := &proto.PushRequest{
		NoticeId:        uuid.New().String(),
		Infohash:        primary,
		ExtraInfohashes: hashes,
		Filename:        form.Filename,
		Work:            form.Work,
		StartedAt:       time.Now().Unix(),
		Email:           form.Email,
		Description:     form.Description,
		Subject:         form.Subject,
		Cause:           proto.PushRequest_Cause(form.Cause),
		Source:          proto.PushRequest_FORM,
	}
	_, err = cl.Push(c.Request.Context(), pr)
	if status.Code(err) == codes.AlreadyExists {
		// Every hash is blocked already: for the reporter the notice went
		// through, it is not a failure to show them.
		return nil
	}
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
		if needsInfohash(form.Cause) && len(extractInfohashes(form.Infohash)) == 0 {
			tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(data).WithErr(web.NewUserError("error.support_infohash_required", errors.New("infohash required for this cause"))))
			return
		}
		if s.ts != nil {
			token := c.PostForm("cf-turnstile-response")
			if err := s.ts.Validate(token, c.ClientIP()); err != nil {
				tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(data).WithErr(errors.Wrap(err, "captcha verification failed")))
				return
			}
		}
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
