package scripts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// warmI18n is one bundle for the parallel tests below: i18n.New sets a
// package global.
var warmI18n = sync.OnceValue(func() *i18n.Service { return i18n.New(os.DirFS("../../locales")) })

type warmFrame struct {
	at   time.Duration
	text string
}

// fakeWarmSeeder serves ?stats with one frame of five peers and ?warmup
// with frames, each at its offset from the request; after the last one the
// stream closes (closeAfter) or stays open until the client leaves.
func fakeWarmSeeder(t *testing.T, frames []warmFrame, closeAfter bool) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		if r.URL.Query().Get("stats") == "true" {
			_, _ = fmt.Fprint(w, "event: statupdate\ndata: {\"peers\":5,\"seeders\":2,\"leechers\":3}\n\n")
			fl.Flush()
			<-r.Context().Done()
			return
		}
		start := time.Now()
		for _, f := range frames {
			select {
			case <-time.After(time.Until(start.Add(f.at))):
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprint(w, f.text)
			fl.Flush()
		}
		if !closeAfter {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/abc/f.mkv?stats=true"
}

// runWarmUp warms 10 MiB of the head (no tail) against frames, and returns
// what warmUp returned and the status lines it drew.
func runWarmUp(t *testing.T, frames []warmFrame, closeAfter bool, slowSec int, ctxTimeout time.Duration) (float64, error, []string) {
	su := fakeWarmSeeder(t, frames, closeAfter)
	s := &ActionScript{api: directAPI(t), c: &web.Context{Lang: "en"}, i18n: warmI18n(),
		warmup: WarmupSettings{TimeoutMin: 1, NoPeersTimeoutSec: 60, SlowPeersTimeoutSec: slowSec}}
	j := job.New(context.Background(), t.Name(), "test", nil, &job.NilStorage{}, false, nil)
	var (
		mu       sync.Mutex
		statuses []string
	)
	o := j.ObserveLog()
	go func() {
		for it := range o.C {
			if it.Level == job.StatusUpdate {
				mu.Lock()
				statuses = append(statuses, it.Status)
				mu.Unlock()
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()
	speed, _, err := s.warmUp(ctx, j, "warming up", su, 700<<20, 10<<20, 0, 2<<20, true)
	mu.Lock()
	defer mu.Unlock()
	return speed, err, slices.Clone(statuses)
}

// A range inside one 16 MiB piece: the chunks arrive, the verified counter
// reads 0 until the piece passes the hash. The viewer reads the chunks (25%,
// not a countdown to "no peers"); the gate still reads the verified counter
// (0: unmeasured).
func TestWarmUp_HaveDrivesTheLineNotTheGate(t *testing.T) {
	t.Parallel()
	speed, err, statuses := runWarmUp(t, []warmFrame{
		{0, "have: 4194304\nspan: 16777216\ndata: 0\n\n"},
		{1500 * time.Millisecond, "have: 16777216\nspan: 16777216\ndata: 0\n\n"},
		{2500 * time.Millisecond, "have: 16777216\nspan: 16777216\ndata: 10485760\n\n"},
	}, true, 60, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(statuses, "25%") {
		t.Errorf("line by have of span: want 25%% among %q", statuses)
	}
	if speed != 0 {
		t.Errorf("the gate's speed is the verified counter's (0 over one piece), got %v", speed)
	}
}

// The verdicts stay on the verified counter; the card's "received" and the
// line follow have, or the verified bytes from a seeder without have/span.
func TestWarmUp_VerdictsStayOnVerified(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		frame      string
		slowSec    int
		ctxTimeout time.Duration
		reason     string
		bytes      int64
		status     string
	}{
		// 3 MiB of chunks is over the slow rule's 1 MiB, the verified
		// 512 KiB under it: still slow.
		{"slow with chunks", "have: 3145728\nspan: 16777216\ndata: 524288\n\n", 2, 10 * time.Second, "slow", 3 << 20, "19%"},
		{"slow, old seeder", "data: 524288\n\n", 2, 10 * time.Second, "slow", 512 << 10, "5%"},
		// The whole piece here, unverified, at the deadline: under skip
		// bytes by the verified counter, so the timeout card.
		{"timeout with every chunk", "have: 16777216\nspan: 16777216\ndata: 0\n\n", 60, 1500 * time.Millisecond, "timeout", 16 << 20, "100%"},
	}
	// slowSec 2: the watchdog's first tick draws the line, the second one
	// rules.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err, statuses := runWarmUp(t, []warmFrame{{0, c.frame}}, false, c.slowSec, c.ctxTimeout)
			var npe *NoPeersError
			if !errors.As(err, &npe) {
				t.Fatalf("want a no-peers verdict, got %v", err)
			}
			if npe.Reason != c.reason || npe.Bytes != c.bytes {
				t.Errorf("verdict %s with %d bytes, want %s with %d", npe.Reason, npe.Bytes, c.reason, c.bytes)
			}
			if !slices.Contains(statuses, c.status) {
				t.Errorf("want %q among %q", c.status, statuses)
			}
		})
	}
}
