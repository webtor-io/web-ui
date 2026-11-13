package j

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-contrib/multitemplate"
	"github.com/urfave/cli"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// A load job's log is kept in the job store and replayed to the next request
// with its id that runs nowhere else (another replica, or this one once the
// run has retired). A bare magnet that ends in a card runs as the shared
// sha1(infohash/lang); the .torrent the card asks for runs as the user's own
// sha1(infohash/lang/uid). The upload must run and reach the resource, not
// replay the card. The private card is not kept at all: it answers the link
// that was submitted (no tracker in it), not the torrent. The dead card is
// kept on purpose, for the next visitor of the same dead magnet.
func TestUploadAfterMagnetCardRunsAFreshLoad(t *testing.T) {
	torrent, hash := privateTorrent(t)
	for _, c := range []struct {
		kind      string
		status    int // rest-api's answer to the magnet
		cacheable bool
	}{
		{"private", http.StatusUnprocessableEntity, false},
		{"dead", http.StatusInternalServerError, true},
	} {
		t.Run(c.kind, func(t *testing.T) {
			st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
			newJobs := testJobs(t, hash, c.status, st)
			replicaA, replicaB := newJobs(), newJobs()
			wc := &web.Context{Lang: "en", ApiClaims: &api.Claims{}}

			card, err := replicaA.Load(wc, &scripts.LoadArgs{Query: "magnet:?xt=urn:btih:" + hash})
			if err != nil {
				t.Fatal(err)
			}
			id := st.waitClosed(t, "the magnet")
			if !hasItem(st.log(id), func(l job.LogItem) bool { return l.Level == job.Custom && l.Body == c.kind+" card" }) {
				t.Fatalf("want the %s card, got %+v", c.kind, st.log(id))
			}
			if card.Cacheable() != c.cacheable {
				t.Errorf("the %s card: Cacheable() = %v, want %v", c.kind, card.Cacheable(), c.cacheable)
			}

			upload := &scripts.LoadArgs{File: torrent, Source: "site"}
			if _, err := replicaB.Load(wc, upload); err != nil {
				t.Fatal(err)
			}
			uploadID := loadJobID(wc, upload, hash)
			if got := st.waitClosed(t, "the upload (it replayed the stored card instead)"); got != uploadID {
				t.Fatalf("the upload's run closed as %s, want %s", got, uploadID)
			}
			l := st.log(uploadID)
			if hasItem(l, func(l job.LogItem) bool { return l.Level == job.Custom }) ||
				!hasItem(l, func(l job.LogItem) bool { return l.Level == job.Redirect && l.Location == "/"+hash }) {
				t.Errorf("want a fresh load that redirects to the resource, got %+v", l)
			}
		})
	}
}

// testJobs makes load-job runners (replicas) that share st, against a fake
// rest-api that knows nothing of hash, answers a magnet with status and
// stores a .torrent. The card is a stub that names its kind.
func testJobs(t *testing.T, hash string, status int, st job.Storage) func() *Jobs {
	t.Helper()
	return testJobsWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource/"+hash:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			body, _ := io.ReadAll(r.Body)
			if bytes.HasPrefix(body, []byte("magnet:")) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"placeholder"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(ra.ResourceResponse{ID: hash})
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}), st)
}

// testJobsWith makes load-job runners (replicas) that share st, against the
// fake rest-api h.
func testJobsWith(t *testing.T, h http.Handler, st job.Storage) func() *Jobs {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	a := api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())

	locales, err := filepath.Abs("../locales")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"templates/layouts/main.html":              `{{ template "main" . }}`,
		"templates/views/load/errors/magnet.html":  `{{ define "main" }}{{ .Data.Kind }} card{{ end }}`,
		"templates/views/load/errors/private.html": `{{ define "main" }}private card {{ .Data.Key }} {{ .Data.Code }}{{ end }}`,
		"templates/partials/.keep":                 ``,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	tb := template.NewManager[*web.Context](multitemplate.NewRenderer()).MustRegisterViews("load/**/*")
	i18nSvc := i18n.New(os.DirFS(locales))
	return func() *Jobs {
		return &Jobs{q: job.NewQueues(st), tb: tb, api: a, enricher: &enrich.Enricher{}, i18n: i18nSvc}
	}
}

// privateTorrent is a .torrent with private=1 and its infohash.
func privateTorrent(t *testing.T) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce"}
	var b bytes.Buffer
	if err := mi.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), mi.HashInfoBytes().HexString()
}

func hasItem(l []job.LogItem, f func(job.LogItem) bool) bool {
	for _, i := range l {
		if f(i) {
			return true
		}
	}
	return false
}

// memStorage is the job store the replicas share (Redis in production).
// closed gets the id of every run whose log it stored to the end: a replay
// stores nothing.
type memStorage struct {
	mu     sync.Mutex
	logs   map[string][]job.LogItem
	closed chan string
}

func (s *memStorage) Pub(_ context.Context, _ string, id string, l job.LogItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs[id] = append(s.logs[id], l)
	if l.Level == job.Close {
		s.closed <- id
	}
	return nil
}

func (s *memStorage) Sub(_ context.Context, _ string, id string) (chan job.LogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.logs[id]) == 0 {
		return nil, nil
	}
	c := make(chan job.LogItem, len(s.logs[id]))
	for _, l := range s.logs[id] {
		c <- l
	}
	close(c)
	return c, nil
}

func (s *memStorage) GetState(context.Context, string, string) (*job.State, bool, error) {
	return nil, false, nil
}

func (s *memStorage) Drop(_ context.Context, _ string, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.logs, id)
	return nil
}

func (s *memStorage) log(id string) []job.LogItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]job.LogItem(nil), s.logs[id]...)
}

func (s *memStorage) waitClosed(t *testing.T, what string) string {
	t.Helper()
	select {
	case id := <-s.closed:
		return id
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no run reached its end in the store", what)
		return ""
	}
}
