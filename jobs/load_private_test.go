package j

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/metrics"
	"github.com/webtor-io/web-ui/services/metrics/metricstest"
	"github.com/webtor-io/web-ui/services/web"
)

// A refusal rest-api names by a code is not a dead magnet: no peer and no
// ten-minute retry change it (Р13). The load job shows the private card with
// the refusal's text and code, never the dead card, and never the link: its
// tr= carries the user's key. The card is not kept for replay. A magnet with
// tr= runs the user's own run, which is never kept anyway; a bare magnet runs
// the shared run, which would replay the card to the next visitor of the
// hash, so there it is the card's own DoNotCache that drops it.
func TestCodedMagnetRefusalIsNotTheDeadCard(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	const uidA = "76543210fedcba9876543210fedcba9876543210"
	withTr := "magnet:?xt=urn:btih:" + h + "&tr=" + url.QueryEscape("http://tracker.example/announce?passkey=00000000000000000000000000000000")
	bare := "magnet:?xt=urn:btih:" + h
	for _, c := range []struct {
		name     string
		query    string
		known    bool // the lookup finds h private; otherwise rest-api does not know it
		status   int
		body     string
		wantCard string
		outcome  string // Р6: a refusal is rejected, a check that did not answer an error
	}{
		{"tracker refused the key", withTr, true, http.StatusUnprocessableEntity,
			`{"error":"x","code":"private_tracker_refused","reason":"key"}`, "private card error.private_tracker_refused private_tracker_refused #log-load", metrics.JobRejected},
		{"verification unavailable", withTr, true, http.StatusServiceUnavailable,
			`{"error":"x","code":"private_verify_unavailable"}`, "private card error.private_verify_unavailable private_verify_unavailable #log-load", metrics.JobError},
		{"429 on a bare magnet, the shared run", bare, false, http.StatusTooManyRequests,
			`{"error":"x"}`, "private card error.private_verify_unavailable rate_limited #log-load", metrics.JobError},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
			jobs := testJobsWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/resource/"+h:
					if !c.known {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte(`{"id":"` + h + `","private":true}`))
				case r.Method == http.MethodPost && r.URL.Path == "/resource":
					w.WriteHeader(c.status)
					_, _ = w.Write([]byte(c.body))
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			}), st)()
			wc := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: uidA}, User: &auth.User{ID: uuid.NewV4()}}
			args := &scripts.LoadArgs{Query: c.query, Source: "site"}
			counted := loadOutcome(t, c.outcome)
			run, err := jobs.Load(wc, args)
			if err != nil {
				t.Fatal(err)
			}
			id := st.waitClosed(t, c.name)
			if want := loadJobID(wc, args, h); id != want {
				t.Fatalf("closed %s, want %s", id, want)
			}
			l := st.log(id)
			if !hasItem(l, func(i job.LogItem) bool {
				return i.Level == job.Custom && i.Template == "load/errors/private" && i.Body == c.wantCard
			}) {
				t.Errorf("want %q, got %+v", c.wantCard, l)
			}
			if hasItem(l, func(i job.LogItem) bool { return i.Template == "load/errors/magnet" }) {
				t.Errorf("the magnet card for a coded refusal: %+v", l)
			}
			b, _ := json.Marshal(l)
			for _, s := range []string{"tracker.example", "passkey"} {
				if strings.Contains(string(b), s) {
					t.Errorf("the log carries %q: %s", s, b)
				}
			}
			if run.Cacheable() {
				t.Error("the private card is kept for replay")
			}
			counted()
		})
	}
}

// An anonymous .torrent upload of a private torrent: rest-api asks for an
// account (422 private_needs_account), and the card says so instead of a red
// line.
func TestAnonUploadNeedsAccountCard(t *testing.T) {
	torrent, h := privateTorrent(t)
	st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
	jobs := testJobsWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/resource" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"x","code":"private_needs_account"}`))
			return
		}
		t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}), st)()
	anon := &web.Context{Lang: "en", ApiClaims: &api.Claims{}}
	args := &scripts.LoadArgs{File: torrent, Source: "site"}
	counted := loadOutcome(t, metrics.JobRejected)
	if _, err := jobs.Load(anon, args); err != nil {
		t.Fatal(err)
	}
	id := st.waitClosed(t, "the upload")
	if want := loadJobID(anon, args, h); id != want {
		t.Fatalf("closed %s, want %s", id, want)
	}
	const want = "private card error.private_needs_account private_needs_account #log-load"
	if l := st.log(id); !hasItem(l, func(i job.LogItem) bool { return i.Level == job.Custom && i.Body == want }) {
		t.Errorf("want %q, got %+v", want, l)
	}
	counted()
}

// loadOutcome checks, once the run has closed, that it was counted once
// under outcome in webui_jobs_total: the card is the answer the user sees,
// not a run that went well (Р6).
func loadOutcome(t *testing.T, outcome string) func() {
	t.Helper()
	count := func() float64 {
		return metricstest.Counter(t, "webui_jobs_total", map[string]string{"job": "load", "outcome": outcome})
	}
	before := count()
	return func() {
		t.Helper()
		if got := count() - before; got != 1 {
			t.Errorf("webui_jobs_total{outcome=%q} moved by %v, want 1", outcome, got)
		}
	}
}
