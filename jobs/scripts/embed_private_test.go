package scripts

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-contrib/multitemplate"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	tpl "github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// The embed never gets uid, so rest-api would answer "sign in". It answers
// with its own card instead, and a private .torrent never reaches rest-api.
func TestEmbedRefusesPrivate(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	private, privateHash := embedTestTorrent(t, true)
	public, publicHash := embedTestTorrent(t, false)
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/private.torrent":
			_, _ = w.Write(private)
		case "/public.torrent":
			_, _ = w.Write(public)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer files.Close()
	// The real client refuses loopback by design.
	orig := embedTorrentClient
	embedTorrentClient = files.Client()
	t.Cleanup(func() { embedTorrentClient = orig })

	type reply struct {
		status int
		body   string
	}
	withTr := "magnet:?xt=urn:btih:" + hash + "&tr=http%3A%2F%2Ftracker.example%2Fannounce"
	bare := "magnet:?xt=urn:btih:" + hash
	cases := []struct {
		name     string
		settings models.EmbedSettings
		api      map[string]reply // "METHOD path" -> rest-api's answer; anything else is an error
		private  bool             // want embed/private
		calls    []string         // the requests rest-api got, in order
	}{
		{name: "torrentUrl, private", settings: models.EmbedSettings{TorrentURL: files.URL + "/private.torrent"}, private: true},
		{
			name:     "torrentUrl, public",
			settings: models.EmbedSettings{TorrentURL: files.URL + "/public.torrent"},
			api: map[string]reply{
				"POST /resource":                        {200, `{"id":"` + publicHash + `"}`},
				"GET /resource/" + publicHash + "/list": {404, ""},
			},
			calls: []string{"POST /resource", "GET /resource/" + publicHash + "/list"},
		},
		{
			name:     "magnet, known private",
			settings: models.EmbedSettings{Magnet: bare},
			api:      map[string]reply{"GET /resource/" + hash: {200, `{"id":"` + hash + `","private":true}`}},
			private:  true,
			calls:    []string{"GET /resource/" + hash},
		},
		{
			name:     "magnet, refused (422)",
			settings: models.EmbedSettings{Magnet: withTr},
			api: map[string]reply{
				"GET /resource/" + hash: {404, ""},
				"POST /resource":        {422, `{"error":"placeholder"}`},
			},
			private: true,
			calls:   []string{"GET /resource/" + hash, "POST /resource"},
		},
		{
			// Only the private kind: a dead magnet is the job's error, as before.
			name:     "magnet, dead",
			settings: models.EmbedSettings{Magnet: withTr},
			api: map[string]reply{
				"GET /resource/" + hash: {404, ""},
				"POST /resource":        {500, `{"error":"magnet timeout"}`},
			},
			calls: []string{"GET /resource/" + hash, "POST /resource"},
		},
		{
			name:     "magnet, known public",
			settings: models.EmbedSettings{Magnet: bare},
			api: map[string]reply{
				"GET /resource/" + hash:           {200, `{"id":"` + hash + `","private":false}`},
				"GET /resource/" + hash + "/list": {404, ""},
			},
			calls: []string{"GET /resource/" + hash, "GET /resource/" + hash + "/list"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				calls []string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := r.Method + " " + r.URL.Path
				mu.Lock()
				calls = append(calls, call)
				mu.Unlock()
				rep, ok := tc.api[call]
				if !ok {
					t.Errorf("unexpected %s", call)
					w.WriteHeader(http.StatusTeapot)
					return
				}
				w.WriteHeader(rep.status)
				_, _ = w.Write([]byte(rep.body))
			}))
			defer srv.Close()
			// Before the chdir of the templates: i18n reads ../../locales now.
			ls := testLoadScript(t, srv, &api.Claims{})
			settings := tc.settings
			s := &EmbedScript{
				api:      ls.api,
				i18n:     ls.i18n,
				settings: &settings,
				tb:       embedTestTemplates(t),
				c:        &web.Context{Lang: "en", ApiClaims: &api.Claims{}},
				dsd:      &embed.DomainSettingsData{},
			}
			j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
			var cards []job.LogItem
			o := j.ObserveLog()
			done := make(chan struct{})
			go func() {
				defer close(done)
				for l := range o.C {
					if l.Level == job.RenderTemplate && l.Template == "embed/private" {
						cards = append(cards, l)
					}
				}
			}()

			err := s.Run(context.Background(), j)
			o.Close()
			<-done

			if tc.private {
				if err != nil {
					t.Errorf("Run: %v, want nil", err)
				}
				want := hash
				if tc.settings.TorrentURL != "" {
					want = privateHash
				}
				if len(cards) != 1 || cards[0].Body != "private "+want {
					t.Errorf("embed/private %+v, want one for %s", cards, want)
				}
			} else if len(cards) != 0 {
				t.Errorf("embed/private %+v, want none", cards)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(calls, tc.calls) {
				t.Errorf("rest-api got %q, want %q", calls, tc.calls)
			}
		})
	}
}

// The real template renders with the link to this server's page of the
// torrent. English only (S32): that every locale has the keys is the parity
// test's job.
func TestEmbedPrivateRenders(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	p, err := template.New("private.html").Funcs(template.FuncMap{
		"t":      helper.T,
		"tpHTML": helper.TpHTML,
		"domain": func() string { return "https://webtor.example" },
	}).ParseFiles("../../templates/views/embed/private.html")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := p.Execute(&b, &web.Context{Lang: "en", Data: hash}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`href="https://webtor.example/` + hash + `"`,
		`target="_blank"`,
		`rel="noopener"`,
		"This torrent does not play here",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// embedTestTemplates is a template manager whose embed/private is a stub
// that names the hash it was given.
func embedTestTemplates(t *testing.T) tpl.Builder[*web.Context] {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "templates/views/embed/private.html")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`private {{ .Data }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return tpl.NewManager[*web.Context](multitemplate.NewRenderer()).MustRegisterViews("embed/*")
}

// embedTestTorrent is a .torrent, private=1 or not, and its infohash.
func embedTestTorrent(t *testing.T, private bool) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20)}
	if private {
		info.Private = &[]bool{true}[0]
	}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce"}
	var b bytes.Buffer
	if err := mi.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), mi.HashInfoBytes().HexString()
}
