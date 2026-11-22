package support

import (
	"context"
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
	log "github.com/sirupsen/logrus"
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
	// The link in abuse-store's confirmation letter. Mail scanners fetch
	// every link in a message, so the GET only shows a button; Confirm runs
	// on its POST, which the session middleware's CSRF check guards.
	r.GET("/support/confirm", h.confirm)
	r.POST("/support/confirm", h.confirm)
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
	// The form is anonymous and scanned before Turnstile: only the first
	// maxInfohashField bytes of the infohash field (a magnet's btih comes
	// before its trackers) and maxScannedDescription bytes of the description
	// are searched for hashes.
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

// isTokenByte: the bytes extractInfohashes reads as one token.
func isTokenByte(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// scanPrefix is the first n bytes of s, cut back to before the token the cut
// falls in: a cut through a longer hex run must not leave its first 40
// characters to read as a hash nobody named.
func scanPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := n
	for i > 0 && isTokenByte(s[i-1]) && isTokenByte(s[i]) {
		i--
	}
	return s[:i]
}

// splitGlued puts a space between hashes pasted into the one-line infohash
// field: an <input type=text> drops the newlines between them, which leaves
// one hex run of 80, 120… characters that extractInfohashes reads as no hash
// at all. A 64-hex v2 hash is not a multiple of 40 and stays whole.
func splitGlued(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && isTokenByte(s[j]) {
			j++
		}
		if j == i {
			b.WriteByte(s[i])
			i++
			continue
		}
		run := s[i:j]
		if len(run) > 40 && len(run)%40 == 0 && strings.Trim(run, "0123456789abcdefABCDEF") == "" {
			for k := 0; k < len(run); k += 40 {
				if k > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(run[k : k+40])
			}
		} else {
			b.WriteString(run)
		}
		i = j
	}
	return b.String()
}

// noticeHashes checks the hashes a POSTed form names and returns them, the
// infohash field's first. Every refusal is a user error for the form.
func noticeHashes(form *Form) ([]string, error) {
	field := splitGlued(scanPrefix(form.Infohash, maxInfohashField))
	if needsInfohash(form.Cause) && len(extractInfohashes(field)) == 0 {
		return nil, web.NewUserError("error.support_infohash_required", errors.New("infohash required for this cause"))
	}
	// The newline keeps the two fields from running into one token.
	desc := scanPrefix(form.Description, maxScannedDescription)
	hashes := extractInfohashes(field + "\n" + desc)
	if len(hashes) > maxNoticeHashes {
		return nil, web.NewUserError("error.support_too_many_hashes", errors.Errorf("notice names more than %d hashes", maxNoticeHashes))
	}
	return hashes, nil
}

type CauseType struct {
	ID  Cause
	Key string
}

// CauseTypes are the form's causes in the order it lists them. An ID is the
// PushRequest.Cause it is sent as and the value links (?cause=) and the form
// post, so it never changes.
var CauseTypes = []CauseType{
	{-1, "support.selectCause"},
	{Cause(proto.PushRequest_ILLEGAL_CONTENT), "support.copyright"},
	{Cause(proto.PushRequest_INTIMATE_IMAGE), "support.intimateImage"},
	{Cause(proto.PushRequest_CSAM_REPORT), "support.csam"},
	{Cause(proto.PushRequest_MALWARE), "support.malware"},
	{Cause(proto.PushRequest_APP_ERROR), "support.siteError"},
	{Cause(proto.PushRequest_QUESTION), "support.question"},
}

type Form struct {
	Cause        Cause  `form:"cause"`
	Subject      string `form:"subject"`
	Description  string `form:"description"`
	Infohash     string `form:"infohash"`
	Filename     string `form:"filename"`
	Email        string `form:"email"`
	Work         string `form:"work"`
	ReporterName string `form:"reporter_name"`
	GoodFaith    bool   `form:"good_faith"`
	Authorized   bool   `form:"authorized"`
}

type Data struct {
	Form       *Form
	CauseTypes []CauseType
	// ConfirmationSent: abuse-store holds the notice until the reporter
	// confirms it from the letter it mailed them.
	ConfirmationSent bool
}

// needsInfohash: every cause but a question is about a specific torrent and
// cannot be acted on without knowing which. Enforced server-side — the form's
// JS-only `required` used to be the only guard, and tickets arrived with the
// field empty.
func needsInfohash(cause Cause) bool {
	return cause >= 0 && cause != Cause(proto.PushRequest_QUESTION)
}

// reporterErr refuses a notice that does not say who sends it, as its cause
// requires: a copyright notice needs the reporter's name, an email and both
// statements; an intimate image report needs an email. Both are what
// abuse-store holds for confirmation from that address, and it refuses them
// the same way; here the reporter learns which field is missing.
func reporterErr(form *Form) error {
	cause := proto.PushRequest_Cause(form.Cause)
	copyright := cause == proto.PushRequest_ILLEGAL_CONTENT
	switch {
	case copyright && strings.TrimSpace(form.ReporterName) == "":
		return web.NewUserError("error.support_name_required", errors.New("copyright notice without the reporter's name"))
	case (copyright || cause == proto.PushRequest_INTIMATE_IMAGE) && strings.TrimSpace(form.Email) == "":
		return web.NewUserError("error.support_email_required", errors.New("notice without the reporter's email"))
	case copyright && !(form.GoodFaith && form.Authorized):
		return web.NewUserError("error.support_statements_required", errors.New("copyright notice without both statements"))
	}
	return nil
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
	if !slices.ContainsFunc(CauseTypes, func(ct CauseType) bool { return ct.ID == Cause(cause) }) {
		return nil, errors.Errorf("cause with id=%v not defined", cause)
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
// when there are none. It reports whether abuse-store holds the notice for
// the reporter to confirm.
func (s *Handler) sendForm(c *gin.Context, form *Form, hashes []string) (bool, error) {
	cl, err := s.asc.Get()
	if err != nil {
		return false, err
	}
	var primary string
	if len(hashes) > 0 {
		primary, hashes = hashes[0], hashes[1:]
	}
	cause := proto.PushRequest_Cause(form.Cause)
	copyright := cause == proto.PushRequest_ILLEGAL_CONTENT
	// The causes abuse-store holds for confirmation, the ones the form asks
	// the reporter's name for.
	held := copyright || cause == proto.PushRequest_INTIMATE_IMAGE
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
		Cause:           cause,
		Source:          proto.PushRequest_FORM,
	}
	// support.js only hides the fields another cause asks for, so the browser
	// still posts what was typed before the cause changed, and the letter
	// prints whatever is set.
	if held {
		pr.ReporterName = form.ReporterName
	}
	if copyright {
		pr.GoodFaith, pr.Authorized = form.GoodFaith, form.Authorized
	}
	rep, err := cl.Push(c.Request.Context(), pr)
	switch status.Code(err) {
	case codes.OK:
		return rep.GetConfirmationSent(), nil
	case codes.AlreadyExists:
		// Every hash is blocked already: for the reporter the notice went
		// through, it is not a failure to show them.
		return false, nil
	case codes.ResourceExhausted:
		// Too many notices wait for confirmation, from this address or in all.
		return false, web.NewUserError("error.support_too_many_pending", err)
	}
	st, ok := status.FromError(err)
	if !ok {
		return false, errors.Wrap(err, "failed to send form")
	}
	return false, errors.New(st.Message())
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
		if err == nil {
			err = reporterErr(form)
		}
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
		data.ConfirmationSent, err = s.sendForm(c, form, hashes)
		if err != nil {
			// Not wrapped: a user error must reach the form as one.
			tpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(data).WithErr(err))
			return
		}
		s.tb.Build("support/success").HTML(http.StatusOK, web.NewContext(c).WithData(data))
		return
	}
	tpl.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

// maxConfirmToken bounds the token taken from the link or the form:
// abuse-store's are 43 characters (32 bytes, unpadded base64url), and a longer
// one cannot be one, so it goes no further.
const maxConfirmToken = 64

// confirmData drives support/confirm: the page with the button while Result
// is empty, the outcome's message key after.
type confirmData struct {
	Token  string
	Result string
}

const confirmInvalid = "support.confirm.invalid"

// confirmResults is the message for each ConfirmReply status; UNKNOWN, and a
// status this build does not know, reads as an invalid link.
var confirmResults = map[proto.ConfirmReply_Status]string{
	proto.ConfirmReply_CONFIRMED:         "support.confirm.confirmed",
	proto.ConfirmReply_ALREADY_CONFIRMED: "support.confirm.alreadyConfirmed",
	proto.ConfirmReply_EXPIRED:           "support.confirm.expired",
}

// confirm serves the confirmation link: the GET renders the button and
// nothing else, the POST confirms. The token is never logged.
func (s *Handler) confirm(c *gin.Context) {
	tpl := s.tb.Build("support/confirm")
	d := &confirmData{Token: c.Query("t")}
	if c.Request.Method == http.MethodPost {
		d.Token = c.PostForm("t")
	}
	ctx := web.NewContext(c).WithData(d)
	if d.Token == "" || len(d.Token) > maxConfirmToken {
		d.Result = confirmInvalid
		tpl.HTML(http.StatusBadRequest, ctx)
		return
	}
	if c.Request.Method != http.MethodPost {
		tpl.HTML(http.StatusOK, ctx)
		return
	}
	res, err := s.confirmNotice(c.Request.Context(), d.Token)
	if err != nil {
		// The token stays usable: the page keeps its button for a retry.
		log.WithError(err).Error("failed to confirm notice")
		tpl.HTML(http.StatusInternalServerError, ctx.WithErrKey("error.support_confirm_failed"))
		return
	}
	d.Result = res
	tpl.HTML(http.StatusOK, ctx)
}

// confirmNotice confirms the notice the token holds and returns the message
// key for the outcome.
func (s *Handler) confirmNotice(ctx context.Context, token string) (string, error) {
	cl, err := s.asc.Get()
	if err != nil {
		return "", err
	}
	rep, err := cl.Confirm(ctx, &proto.ConfirmRequest{Token: token})
	if err != nil {
		return "", errors.Wrap(err, "failed to confirm notice")
	}
	if key, ok := confirmResults[rep.GetStatus()]; ok {
		return key, nil
	}
	return confirmInvalid, nil
}
