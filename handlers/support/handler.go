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
	infohashRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	percentEscapeRe = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
)

const (
	// maxNoticeHashes: a form notice bans every hash it names without any
	// check of the reporter, so the cap bounds how many torrents one false
	// notice takes down. A notice naming more is refused whole, and the
	// reporter is asked to split it.
	maxNoticeHashes = 20
	// The form is anonymous and scanned before Turnstile: an infohash field
	// longer than maxInfohashField is refused unread, and only the first
	// maxScannedDescription bytes of the description are searched for hashes.
	maxInfohashField      = 4 << 10
	maxScannedDescription = 64 << 10
)

// extractInfohashes pulls every v1 infohash out of whatever the reporter
// pasted: bare hashes, magnet links (hex or base32), webtor URLs, a list in
// the description. Lower-cased, first-seen order, no repeats, and at most
// maxNoticeHashes+1: it stops scanning at the first hash past the cap, which
// is all the caller needs to refuse the notice.
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
	// The text is walked as maximal runs of ASCII letters and digits, so a
	// hash counts only when bounded by something else or the text's edge: a
	// 64-hex v2 hash or a longer hex run is one token and yields nothing. A
	// byte loop, not a regexp: a token costs no allocation.
	s := strings.ToLower(in)
	var hashes []string
	btihValue := -1 // where the value after the last "btih:" starts
	for i := 0; i < len(s) && len(hashes) <= maxNoticeHashes; {
		j := i
		for j < len(s) && ('0' <= s[j] && s[j] <= '9' || 'a' <= s[j] && s[j] <= 'z') {
			j++
		}
		if j == i {
			i++
			continue
		}
		h := s[i:j]
		if h == "btih" && strings.HasPrefix(s[j:], ":") {
			btihValue = j + 1
		}
		// 32 characters right after "btih:" is a base32 magnet. A 40-character
		// value stays hex even when every digit is also a base32 letter.
		if i == btihValue && len(h) == 32 {
			if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(h)); err == nil {
				h = hex.EncodeToString(b)
			}
		}
		i = j
		if !infohashRe.MatchString(h) || slices.Contains(hashes, h) {
			continue
		}
		hashes = append(hashes, h)
	}
	return hashes
}

// noticeHashes checks the hashes a POSTed form names and returns them, the
// infohash field's first. Every refusal is a user error for the form.
func noticeHashes(form *Form) ([]string, error) {
	if len(form.Infohash) > maxInfohashField {
		return nil, web.NewUserError("error.support_infohash_too_long", errors.New("infohash field too long"))
	}
	if needsInfohash(form.Cause) && len(extractInfohashes(form.Infohash)) == 0 {
		return nil, web.NewUserError("error.support_infohash_required", errors.New("infohash required for this cause"))
	}
	// The newline keeps the two fields from running into one token.
	desc := form.Description[:min(len(form.Description), maxScannedDescription)]
	hashes := extractInfohashes(form.Infohash + "\n" + desc)
	if len(hashes) > maxNoticeHashes {
		return nil, web.NewUserError("error.support_too_many_hashes", errors.Errorf("notice names more than %d hashes", maxNoticeHashes))
	}
	return hashes, nil
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

// sendForm pushes one notice that bans every hash it names: the first is the
// primary, the rest go as ExtraInfohashes, which is empty (nil once decoded)
// when there are none.
func (s *Handler) sendForm(c *gin.Context, form *Form, hashes []string) error {
	cl, err := s.asc.Get()
	if err != nil {
		return err
	}
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
		hashes, err := noticeHashes(form)
		if err != nil {
			tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(data).WithErr(err))
			return
		}
		if s.ts != nil {
			token := c.PostForm("cf-turnstile-response")
			if err := s.ts.Validate(token, c.ClientIP()); err != nil {
				tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(data).WithErr(errors.Wrap(err, "captcha verification failed")))
				return
			}
		}
		err = s.sendForm(c, form, hashes)
		if err != nil {
			tpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(data).WithErr(errors.Wrap(err, "failed to send form")))
			return
		}
		s.tb.Build("support/success").HTML(http.StatusOK, web.NewContext(c).WithData(data))
		return
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}
