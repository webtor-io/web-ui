package scripts

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/web"
)

func fileInfoTemplate(t *testing.T) *template.Template {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New("file_info.html").Funcs(template.FuncMap{
		"t":             helper.T,
		"tp":            helper.Tp,
		"langDisplayIn": stremio.NewLangDisplayIn,
	}).ParseFiles("../../templates/views/action/file_info.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

// The card says what the file holds in every locale, escapes the file's own
// free text, and carries no URL: the job log replaying it is public.
func TestFileInfoCardRenders(t *testing.T) {
	tpl := fileInfoTemplate(t)
	type ctx struct {
		Lang string
		Data *FileInfo
	}
	full := &FileInfo{
		Duration: "1:52:03",
		Video:    []FileInfoVideo{{Codec: "HEVC", Width: 3840, Height: 2160, HDR: "HDR10", Mbps: 8.44}},
		Audio: []FileInfoTrack{
			{Lang: "ru", Title: "Dub", Codec: "EAC3", Channels: "5.1(side)", Default: true},
			{Codec: "AC3", Channels: "stereo"},
		},
		Subtitles: []FileInfoTrack{
			{Lang: "en", Codec: "PGS", Picture: true},
			{Lang: "en", Title: `<script>alert(1)</script>`, Codec: "SRT", SDH: true, Default: true},
			{Lang: "fr", Codec: "SRT", Forced: true},
			{Lang: "is", Codec: "SRT"},
		},
	}
	out := renderModal(t, tpl, "en", &ctx{Lang: "en", Data: full})
	for _, w := range []string{
		"File info", "Duration: 1:52:03",
		"HEVC", "3840×2160", "HDR10", "8.4\u00a0Mbps",
		"Russian", "EAC3 · 5.1(side)", "Language not specified",
		"English", "French", "IS", "SDH", "signs only", "picture-based · external player only",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"external player",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q:\n%s", w, out)
		}
	}
	if n := strings.Count(out, "data-file-info-default"); n != 2 {
		t.Errorf("default badges: %d, want one audio and one subtitle", n)
	}
	for _, b := range []string{"<script>", "https://", "token", "No subtitles", "No audio"} {
		if strings.Contains(out, b) {
			t.Errorf("must not contain %q", b)
		}
	}

	empty := renderModal(t, tpl, "en", &ctx{Lang: "en", Data: &FileInfo{}})
	for _, w := range []string{"No audio tracks in the file", "No subtitles inside the file"} {
		if !strings.Contains(empty, w) {
			t.Errorf("empty probe: missing %q", w)
		}
	}
	for _, b := range []string{">Video<", "Duration"} {
		if strings.Contains(empty, b) {
			t.Errorf("empty probe: must not contain %q", b)
		}
	}

	// Captions in the video stream and no subtitle track: the row, not "no
	// subtitles".
	cc := renderModal(t, tpl, "en", &ctx{Lang: "en", Data: &FileInfo{ClosedCaptions: true}})
	if !strings.Contains(cc, "Closed captions (CC) in the video stream") || strings.Contains(cc, "No subtitles") {
		t.Errorf("closed captions:\n%s", cc)
	}

	ru := renderModal(t, tpl, "ru", &ctx{Lang: "ru", Data: full})
	for _, w := range []string{"О файле", "русский", "Язык не указан", "8,4\u00a0Мбит/с", "по умолчанию"} {
		if !strings.Contains(ru, w) {
			t.Errorf("ru: missing %q", w)
		}
	}
	for _, lang := range i18n.SupportedLangs {
		for _, d := range []*FileInfo{full, {}, {ClosedCaptions: true}} {
			if o := renderModal(t, tpl, lang, &ctx{Lang: lang, Data: d}); strings.Contains(o, "fileInfo.") || strings.Contains(o, "action.stream.") || strings.Contains(o, "resource.status.") {
				t.Errorf("lang=%s: unresolved key:\n%s", lang, o)
			}
		}
	}
}

// probeError tells the viewer whose failure it is: ffprobe's own failure is
// the file's, a cut-off probe or an unreachable prober is ours, and our own
// deadline stays a deadline -- the no-peers card with a retry.
func TestFileInfoProbeError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New(`media probe status=500 body="probing failed: exit status 1"`), "error.probe_failed"},
		{errors.New(`media probe status=500 body="probing failed: context canceled"`), "error.upstream_unavailable"},
		{errors.New(`media probe status=500 body="probing failed: context deadline exceeded"`), "error.upstream_unavailable"},
		{errors.New(`media probe status=500 body=""`), "error.upstream_unavailable"},
		{errors.New(`media probe status=502 body="Bad Gateway"`), "error.upstream_unavailable"},
		{errors.New(`media probe status=503 body="Service Unavailable"`), "error.upstream_unavailable"},
		{errors.Wrap(errors.New("dial tcp: connection refused"), "failed to do request"), "error.upstream_unavailable"},
	}
	for _, c := range cases {
		if got := web.ClassifyError(probeError(c.err)); got != c.want {
			t.Errorf("%v: %s, want %s", c.err, got, c.want)
		}
	}

	// The deadline as GetMediaProbe really returns it: a client call that
	// outlives its context, wrapped the way api.GetMediaProbe wraps it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("hanging server answered")
	}
	err = probeError(errors.Wrap(err, "failed to do request"))
	// ErrorWrapperScript's own test for the no-peers card.
	if !errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		t.Errorf("deadline lost: %v", err)
	}
}
