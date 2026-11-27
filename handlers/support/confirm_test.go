package support

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/abuse-store/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webtor-io/web-ui/handlers/session"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
)

// A token as abuse-store mints them: 32 bytes, unpadded base64url.
const confirmTok = "q5Xh2wJ0-ZbLrT9e_KdM3pVn8sYa1cFgUoIiE7lN4Ow"

func newConfirmStore() *fakeAbuseStore {
	return &fakeAbuseStore{got: make(chan *proto.PushRequest, 1), confirms: make(chan string, 1)}
}

// confirmed is the token of the Confirm the request made, and fails the test
// when it made none: a bare receive would hang the package until go test's
// timeout instead.
func confirmed(t *testing.T, as *fakeAbuseStore) string {
	t.Helper()
	select {
	case tok := <-as.confirms:
		return tok
	case <-time.After(5 * time.Second):
		t.Fatal("Confirm was not called")
		return ""
	}
}

// localesDir is absolute: newSupportRouter moves the test into a temp dir.
var localesDir, _ = filepath.Abs("../../locales")

// english is the page text of a message key, as the template escapes it.
func english(t *testing.T, key string) string {
	t.Helper()
	locales, err := os.OpenRoot(localesDir)
	if err != nil {
		t.Fatal(err)
	}
	defer locales.Close()
	return template.HTMLEscapeString(i18n.NewHelper(i18n.New(locales.FS())).T("en", key))
}

func postConfirm(r *gin.Engine, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/support/confirm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	r.ServeHTTP(w, req)
	return w
}

// The link in the letter only shows the button: a mail scanner fetching it
// confirms nothing.
func TestSupportConfirmGetHasNoSideEffect(t *testing.T) {
	as := newConfirmStore()
	r := newSupportRouter(t, as)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/support/confirm?t="+confirmTok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`method="post"`, `action="/support/confirm"`, `name="t" value="` + confirmTok + `"`, english(t, "support.confirm.button")} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if len(as.confirms) != 0 {
		t.Fatal("the GET confirmed the notice")
	}
}

// Every Confirm status reads its own message; the token goes through as is.
func TestSupportConfirmStatuses(t *testing.T) {
	for st, key := range map[proto.ConfirmReply_Status]string{
		proto.ConfirmReply_CONFIRMED:         "support.confirm.confirmed",
		proto.ConfirmReply_ALREADY_CONFIRMED: "support.confirm.alreadyConfirmed",
		proto.ConfirmReply_EXPIRED:           "support.confirm.expired",
		proto.ConfirmReply_UNKNOWN:           "support.confirm.invalid",
		proto.ConfirmReply_Status(99):        "support.confirm.invalid",
	} {
		t.Run(st.String(), func(t *testing.T) {
			as := newConfirmStore()
			as.confirm = &proto.ConfirmReply{Status: st, Hashes: 2}
			r := newSupportRouter(t, as)
			w := postConfirm(r, url.Values{"t": {confirmTok}})
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), english(t, key)) {
				t.Fatalf("got %d, want 200 with %s:\n%s", w.Code, key, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `<form`) {
				t.Error("an answered confirmation still shows the button")
			}
			if got := confirmed(t, as); got != confirmTok {
				t.Errorf("confirmed %q, want %q", got, confirmTok)
			}
		})
	}
}

// A failed Confirm leaves the token usable: the page says so and keeps the
// button for a retry.
func TestSupportConfirmFailureKeepsTheButton(t *testing.T) {
	as := newConfirmStore()
	as.confirmErr = status.Error(codes.Internal, "ban failed")
	r := newSupportRouter(t, as)
	w := postConfirm(r, url.Values{"t": {confirmTok}})
	body := w.Body.String()
	if w.Code != http.StatusInternalServerError || !strings.Contains(body, english(t, "error.support_confirm_failed")) {
		t.Fatalf("got %d, want 500 with the failure:\n%s", w.Code, body)
	}
	if !strings.Contains(body, `name="t" value="`+confirmTok+`"`) {
		t.Error("the failure page has no button to retry with")
	}
}

// A token is at most maxConfirmToken characters: a longer or a missing one is
// an invalid link and reaches no Confirm, on the GET or the POST.
func TestSupportConfirmBoundsTheToken(t *testing.T) {
	long := strings.Repeat("A", maxConfirmToken+1)
	for name, tok := range map[string]string{"oversized": long, "missing": ""} {
		t.Run(name, func(t *testing.T) {
			as := newConfirmStore()
			r := newSupportRouter(t, as)
			g := httptest.NewRecorder()
			r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/support/confirm?t="+tok, nil))
			p := postConfirm(r, url.Values{"t": {tok}})
			for method, w := range map[string]*httptest.ResponseRecorder{"GET": g, "POST": p} {
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), english(t, "support.confirm.invalid")) {
					t.Errorf("%s: got %d, want 400 with the invalid link:\n%s", method, w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), `<form`) {
					t.Errorf("%s: an invalid link shows the button", method)
				}
			}
			if len(as.confirms) != 0 {
				t.Fatal("an invalid token reached Confirm")
			}
		})
	}
	t.Run("at the bound", func(t *testing.T) {
		as := newConfirmStore()
		as.confirm = &proto.ConfirmReply{}
		r := newSupportRouter(t, as)
		postConfirm(r, url.Values{"t": {long[1:]}})
		if got := confirmed(t, as); got != long[1:] {
			t.Errorf("confirmed %q", got)
		}
	})
}

var csrfField = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

// The POST is behind the session's CSRF check: without the token the page
// hands out it confirms nothing, with it it does.
func TestSupportConfirmNeedsCSRF(t *testing.T) {
	as := newConfirmStore()
	as.confirm = &proto.ConfirmReply{Status: proto.ConfirmReply_CONFIRMED}
	r := newSupportRouter(t, as, func(r *gin.Engine) {
		app := cli.NewApp()
		app.Flags = session.RegisterFlags([]cli.Flag{
			cli.StringFlag{Name: common.SessionSecretFlag},
			cli.StringFlag{Name: common.DomainFlag},
		})
		app.Action = func(c *cli.Context) error { return session.RegisterHandler(c, r, nil) }
		if err := app.Run([]string{"x", "--" + common.SessionSecretFlag, "0123456789abcdef0123456789abcdef",
			"--" + common.DomainFlag, "http://localhost"}); err != nil {
			t.Fatal(err)
		}
	})

	g := httptest.NewRecorder()
	r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/support/confirm?t="+confirmTok, nil))
	m := csrfField.FindStringSubmatch(g.Body.String())
	if g.Code != http.StatusOK || m == nil {
		t.Fatalf("got %d with no CSRF field:\n%s", g.Code, g.Body.String())
	}
	cookies := g.Result().Cookies()

	if w := postConfirm(r, url.Values{"t": {confirmTok}}, cookies...); w.Code != http.StatusBadRequest || len(as.confirms) != 0 {
		t.Fatalf("without the CSRF token: got %d, %d confirmed; want 400, none", w.Code, len(as.confirms))
	}
	w := postConfirm(r, url.Values{"t": {confirmTok}, "_csrf": {m[1]}}, cookies...)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), english(t, "support.confirm.confirmed")) {
		t.Fatalf("with the page's CSRF token: got %d:\n%s", w.Code, w.Body.String())
	}
	if got := confirmed(t, as); got != confirmTok {
		t.Errorf("confirmed %q", got)
	}
}
