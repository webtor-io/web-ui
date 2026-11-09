package scripts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// A seeder sends a stats frame only when something changed, so the flag can be
// in the first frame (the torrent was already loaded), which the probe
// consumes, or in a later one (the announce failed after the warm-up started).
func TestWarmUp_DeadSwarmVerdictReadsTrackerRefused(t *testing.T) {
	t.Parallel()
	const (
		plain   = `{"peers":0,"seeders":0,"leechers":0}`
		refused = `{"peers":0,"seeders":0,"leechers":0,"tracker_refused":true}`
		cleared = `{"peers":0,"seeders":0,"leechers":0,"tracker_refused":false}`
	)
	cases := []struct {
		name   string
		frames []string
		reason string
	}{
		{"refused in the first frame", []string{refused}, "refused"},
		{"refused in a later frame", []string{plain, refused}, "refused"},
		{"seeder without the field", []string{plain}, "dead"},
		{"tracker recovered before the verdict", []string{refused, cleared}, "dead"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				if r.URL.Query().Get("stats") == "true" {
					for _, f := range c.frames {
						_, _ = fmt.Fprintf(w, "event: statupdate\ndata: %s\n\n", f)
						fl.Flush()
						time.Sleep(50 * time.Millisecond)
					}
				}
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)

			s := &ActionScript{api: directAPI(t), c: &web.Context{Lang: "en"}, i18n: warmI18n(),
				warmup: WarmupSettings{TimeoutMin: 1, NoPeersTimeoutSec: 1, SlowPeersTimeoutSec: 60, SeederProbeTimeoutSec: 1}}
			j := job.New(context.Background(), t.Name(), "test", nil, &job.NilStorage{}, false, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _, err := s.warmUp(ctx, j, "warming up", srv.URL+"/abc/f.mkv?stats=true", 700<<20, 10<<20, 0, 2<<20, true, log.Fields{"phase": "test"})

			var npe *NoPeersError
			if !errors.As(err, &npe) {
				t.Fatalf("want a no-peers verdict, got %v", err)
			}
			if npe.Reason != c.reason {
				t.Errorf("reason %q, want %q", npe.Reason, c.reason)
			}
		})
	}
}
