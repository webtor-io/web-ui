package scripts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-contrib/multitemplate"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/private_torrent"
	tpl "github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// The run itself fills Own from the seeder's reason and the user's claims
// (Р14): a refused key after enforce, for a user with a uid, draws the
// own-key card.
func TestNoPeersOwnKeyFromTheRun(t *testing.T) {
	private_torrent.SetEnforce(true)
	t.Cleanup(func() { private_torrent.SetEnforce(false) })
	dir := t.TempDir()
	p := filepath.Join(dir, "templates/views/action/errors/no_peers.html")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{{ define "main" }}own={{ .Data.Own }}{{ end }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	s := &ErrorWrapperScript{
		tb:     tpl.NewManager[*web.Context](multitemplate.NewRenderer()).MustRegisterViews("action/**/*"),
		Script: job.NewScript(func(*job.Job) error { return &NoPeersError{Reason: "refused"} }),
		c:      &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"}},
		action: "stream-video",
	}
	j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
	o := j.ObserveLog()
	var bodies []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for l := range o.C {
			if l.Level == job.Custom && l.Template == "action/errors/no_peers" {
				bodies = append(bodies, l.Body)
			}
		}
	}()
	err := s.Run(context.Background(), j)
	o.Close()
	<-done
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(bodies) != 1 || bodies[0] != "own=true" {
		t.Errorf("no-peers card %q, want one with own=true", bodies)
	}
}
