package support

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/abuse-store/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webtor-io/web-ui/services/abuse_store"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	hashA = "c9e15763f722f23e98a29decdfae341b98d53056"
	hashB = "0123456789abcdef0123456789abcdef01234567"
	hashC = "fedcba9876543210fedcba9876543210fedcba98"
	// Every digit is also a base32 letter: still a hex hash after "btih:".
	hashBase32Letters = "abcdef234567abcdef234567abcdef234567abcd"
	// hashA in base32, as some magnet links carry it.
	hashABase32 = "ZHQVOY7XELZD5GFCTXWN7LRUDOMNKMCW"
)

// A notice is enforced for every v1 infohash it names and for nothing else.
// The old unanchored first match turned "btih%3A<hash>" into a hash shifted
// by two characters and dropped every hash after the first.
func TestExtractInfohashes(t *testing.T) {
	v2 := strings.Repeat("ab", 32)
	var many []string
	for i := 1; i <= 150; i++ {
		many = append(many, fmt.Sprintf("%040x", i))
	}
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"bare hash", hashA, []string{hashA}},
		{"bare upper-case hash", strings.ToUpper(hashA), []string{hashA}},
		{"webtor link", "https://webtor.io/" + hashA + "?file=a.mkv", []string{hashA}},
		{"webtor link with lang and file", "https://webtor.io/ru/" + hashA + "/Some.File.mkv", []string{hashA}},
		{"magnet hex", "magnet:?xt=urn:btih:" + hashA + "&dn=x", []string{hashA}},
		{"upper-case magnet", "magnet:?xt=urn:btih:" + strings.ToUpper(hashA) + "&dn=x", []string{hashA}},
		{"encoded magnet", "magnet%3A%3Fxt%3Durn%3Abtih%3A" + hashA + "%26dn%3Dx", []string{hashA}},
		{"double-encoded magnet", "magnet%253A%253Fxt%253Durn%253Abtih%253A" + hashA, []string{hashA}},
		{"triple-encoded magnet", "btih%25253A" + hashA, []string{hashA}},
		// Only % and two hex digits is an escape: anything else stays text.
		{"literal percent sign", "100% " + hashA, []string{hashA}},
		{"malformed escape", "%zz " + hashA, []string{hashA}},
		{"malformed escape glued to a hash", "%zz" + hashA, nil},
		{"base32 magnet", "magnet:?xt=urn:btih:" + hashABase32 + "&dn=x", []string{hashA}},
		{"lower-case base32 magnet", "magnet:?xt=urn:btih:" + strings.ToLower(hashABase32), []string{hashA}},
		{"base32 without btih:", hashABase32, nil},
		{"base32 not right after btih:", "btih: " + hashABase32, nil},
		{"hex magnet made of base32 letters", "magnet:?xt=urn:btih:" + hashBase32Letters, []string{hashBase32Letters}},
		{"v2 hash", v2, nil},
		{"v2 magnet", "magnet:?xt=urn:btmh:1220" + v2, nil},
		{"longer hex run", hashA + "ab", nil},
		{"glued to a letter", "x" + hashA, nil},
		{"two hashes one space apart", hashA + " " + hashB, []string{hashA, hashB}},
		{"first-seen order", hashC + "\n" + hashA + "\n" + hashB, []string{hashC, hashA, hashB}},
		{"duplicates", hashA + "\n" + strings.ToUpper(hashA) + " magnet:?xt=urn:btih:" + hashABase32, []string{hashA}},
		// One past the cap, so the caller can refuse the notice.
		{"cap", strings.Join(many, " "), many[:maxNoticeHashes+1]},
		{"no hash at all", "please remove my movie", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractInfohashes(tc.in); !slices.Equal(got, tc.want) {
				t.Fatalf("extracted %q, want %q", got, tc.want)
			}
		})
	}
}

type fakeAbuseStore struct {
	proto.UnimplementedAbuseStoreServer
	got chan *proto.PushRequest
	err error
}

func (f *fakeAbuseStore) Push(_ context.Context, in *proto.PushRequest) (*proto.PushReply, error) {
	f.got <- in
	if f.err != nil {
		return nil, f.err
	}
	return &proto.PushReply{}, nil
}

// newSupportRouter serves /support over stand-in views, with the handler's
// abuse-store client dialing a fake gRPC server.
func newSupportRouter(t *testing.T, as *fakeAbuseStore) *gin.Engine {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	proto.RegisterAbuseStoreServer(gs, as)
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(gs.Stop)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range abuse_store.RegisterFlags(nil) {
		f.Apply(fs)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if err := fs.Parse([]string{"--" + abuse_store.UseFlag, "--" + abuse_store.HostFlag, "127.0.0.1", "--" + abuse_store.PortFlag, port}); err != nil {
		t.Fatal(err)
	}
	asc := abuse_store.New(cli.NewContext(nil, fs, nil))
	t.Cleanup(asc.Close)

	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	svc := i18n.New(locales.FS())
	dir := t.TempDir()
	for name, body := range map[string]string{
		"templates/layouts/main.html":          `{{ template "main" . }}`,
		"templates/views/support/form.html":    `{{ define "main" }}[form][err={{ .ErrKey }}]{{ end }}`,
		"templates/views/support/success.html": `{{ define "main" }}[success]{{ end }}`,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates/partials"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(i18n.GinMiddleware(svc))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	RegisterHandler(r, tm, asc, nil)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func postSupport(r *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/support", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	return w
}

// One notice carries every hash the reporter named: the infohash field's
// first is the primary, the rest of both fields go as extras.
func TestSupportPushesEveryHash(t *testing.T) {
	cases := []struct {
		name            string
		cause, ih, desc string
		primary         string
		extras          []string
		wantCause       proto.PushRequest_Cause
	}{
		{
			name:    "primary from the infohash field",
			cause:   "0",
			ih:      "magnet%3A%3Fxt%3Durn%3Abtih%3A" + hashA + " " + hashC,
			desc:    "also https://webtor.io/" + hashB + " and " + hashA,
			primary: hashA, extras: []string{hashC, hashB},
			wantCause: proto.PushRequest_ILLEGAL_CONTENT,
		},
		{
			name:    "primary from the description",
			cause:   "3",
			desc:    hashB + ", " + hashA,
			primary: hashB, extras: []string{hashA},
			wantCause: proto.PushRequest_QUESTION,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
			r := newSupportRouter(t, as)
			w := postSupport(r, url.Values{"cause": {tc.cause}, "infohash": {tc.ih}, "description": {tc.desc}})
			if w.Code != http.StatusOK || w.Body.String() != "[success]" {
				t.Fatalf("got %d %q, want 200 [success]", w.Code, w.Body.String())
			}
			pr := <-as.got
			if pr.GetInfohash() != tc.primary || !slices.Equal(pr.GetExtraInfohashes(), tc.extras) {
				t.Errorf("pushed %q + %q, want %q + %q", pr.GetInfohash(), pr.GetExtraInfohashes(), tc.primary, tc.extras)
			}
			if pr.GetSource() != proto.PushRequest_FORM || pr.GetCause() != tc.wantCause || pr.GetDescription() != tc.desc {
				t.Errorf("pushed source %v cause %v description %q", pr.GetSource(), pr.GetCause(), pr.GetDescription())
			}
		})
	}
}

// abuse-store answers AlreadyExists only when every hash is blocked already:
// the reporter sees the usual success page, any other error stays a failure.
func TestSupportPushErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		body   string
	}{
		{status.Error(codes.AlreadyExists, "abuse notice with infoHash="+hashA+" already exists"), http.StatusOK, "[success]"},
		{status.Error(codes.InvalidArgument, "wrong email"), http.StatusInternalServerError, "[form]"},
	}
	for _, tc := range cases {
		t.Run(status.Code(tc.err).String(), func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1), err: tc.err}
			r := newSupportRouter(t, as)
			w := postSupport(r, url.Values{"cause": {"0"}, "infohash": {hashA}})
			if w.Code != tc.status || !strings.HasPrefix(w.Body.String(), tc.body) {
				t.Fatalf("got %d %q, want %d %s", w.Code, w.Body.String(), tc.status, tc.body)
			}
		})
	}
}

// A complaint still needs a hash in the infohash field itself; one only in
// the description does not satisfy it, and nothing is pushed.
func TestSupportNeedsHashInInfohashField(t *testing.T) {
	as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
	r := newSupportRouter(t, as)
	w := postSupport(r, url.Values{"cause": {"0"}, "infohash": {"see description"}, "description": {hashA}})
	if w.Code != http.StatusBadRequest || w.Body.String() != "[form][err=error.support_infohash_required]" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if len(as.got) != 0 {
		t.Fatal("pushed a notice the form rejected")
	}
}

// The form is anonymous and scanned before Turnstile, so what one POST costs
// must not grow with its size, nor with the tokens it holds: matching every
// token of a 40 MiB token-dense description took the heap from 76 MB to
// 1785 MB.
func TestNoticeHashesBoundedWork(t *testing.T) {
	dense := strings.Repeat("a ", 8<<20) // 16 MiB, a token every 2 bytes
	cases := []struct {
		name    string
		form    *Form
		want    []string
		wantErr string
	}{
		{"token-dense description", &Form{Infohash: hashA, Description: dense}, []string{hashA}, ""},
		{"token-dense infohash field", &Form{Infohash: dense}, nil, "error.support_infohash_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			got, err := noticeHashes(tc.form)
			runtime.ReadMemStats(&after)
			if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
				t.Errorf("allocated %d bytes for %d bytes of input", n, len(tc.form.Infohash)+len(tc.form.Description))
			}
			var key string
			if err != nil {
				key = web.ClassifyError(err)
			}
			if !slices.Equal(got, tc.want) || key != tc.wantErr {
				t.Errorf("got %q, error %q; want %q, error %q", got, key, tc.want, tc.wantErr)
			}
		})
	}
}

// The form says 20 in every language: a notice naming 20 distinct hashes goes
// through, one naming 21 pushes nothing and asks the reporter to split it.
func TestSupportHashCap(t *testing.T) {
	var hs []string
	for i := 1; i <= 21; i++ {
		hs = append(hs, fmt.Sprintf("%040x", i))
	}
	for _, n := range []int{20, 21} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
			r := newSupportRouter(t, as)
			// The field's hash repeated in the description is not another one.
			w := postSupport(r, url.Values{"cause": {"0"}, "infohash": {hs[0]}, "description": {strings.Join(hs[:n], "\n")}})
			if n == 21 {
				if w.Code != http.StatusBadRequest || w.Body.String() != "[form][err=error.support_too_many_hashes]" || len(as.got) != 0 {
					t.Fatalf("got %d %q, %d pushed; want 400, the split error, nothing pushed", w.Code, w.Body.String(), len(as.got))
				}
				return
			}
			if w.Code != http.StatusOK || w.Body.String() != "[success]" {
				t.Fatalf("got %d %q, want 200 [success]", w.Code, w.Body.String())
			}
			if pr := <-as.got; pr.GetInfohash() != hs[0] || !slices.Equal(pr.GetExtraInfohashes(), hs[1:n]) {
				t.Errorf("pushed %q + %q", pr.GetInfohash(), pr.GetExtraInfohashes())
			}
		})
	}
}

// An infohash field over 4 KiB is refused before anything scans it; one at
// 4 KiB goes through.
func TestSupportInfohashFieldBound(t *testing.T) {
	for _, n := range []int{4 << 10, 4<<10 + 1} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
			r := newSupportRouter(t, as)
			w := postSupport(r, url.Values{"cause": {"0"}, "infohash": {hashA + strings.Repeat(" ", n-len(hashA))}})
			want, body := http.StatusOK, "[success]"
			if n > 4<<10 {
				want, body = http.StatusBadRequest, "[form][err=error.support_infohash_too_long]"
			}
			if w.Code != want || w.Body.String() != body {
				t.Fatalf("got %d %q, want %d %q", w.Code, w.Body.String(), want, body)
			}
		})
	}
}
