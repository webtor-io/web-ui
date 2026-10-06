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

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"

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
// what warmUp returned, the status lines it drew and its "warmup measured"
// line.
func runWarmUp(t *testing.T, frames []warmFrame, closeAfter bool, slowSec int, ctxTimeout time.Duration) (float64, error, []string, *log.Entry) {
	hook := logtest.NewGlobal()
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
	speed, _, err := s.warmUp(ctx, j, "warming up", su, 700<<20, 10<<20, 0, 2<<20, true, log.Fields{"phase": "test"})
	var line *log.Entry
	for _, e := range hook.AllEntries() {
		if e.Message == "warmup measured" && e.Data["job"] == t.Name() {
			if line != nil {
				t.Errorf("more than one warmup measured line")
			}
			line = e
		}
	}
	mu.Lock()
	defer mu.Unlock()
	return speed, err, slices.Clone(statuses), line
}

// A range inside one 16 MiB piece: the chunks arrive, the verified counter
// reads 0 until the piece passes the hash. The viewer reads the chunks (25%,
// not a countdown to "no peers"); the gate still reads the verified counter
// (0: unmeasured), and the measured line carries both speeds.
func TestWarmUp_HaveDrivesTheLineNotTheGate(t *testing.T) {
	t.Parallel()
	speed, err, statuses, line := runWarmUp(t, []warmFrame{
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
	if line == nil {
		t.Fatal("no warmup measured line")
	}
	if line.Level != log.InfoLevel {
		t.Errorf("level %v", line.Level)
	}
	d := line.Data
	for k, want := range map[string]any{
		"phase": "test", "outcome": "done", "target": int64(10 << 20),
		"span": int64(16 << 20), "have": int64(16 << 20), "data": int64(10 << 20),
		"have0": int64(4 << 20), "data0": int64(10 << 20), "speed_verified": float64(0),
	} {
		if d[k] != want {
			t.Errorf("%s = %v (%T), want %v", k, d[k], d[k], want)
		}
	}
	// 12 MiB over ~2.5 s.
	if v, _ := d["speed_have"].(float64); v < 4<<20 || v > 6<<20 {
		t.Errorf("speed_have = %v, want ~4.8 MiB/s", d["speed_have"])
	}
	// have reached span at 1.5 s, the close came at 2.5 s.
	if v, _ := d["hash_wait"].(time.Duration); v < 500*time.Millisecond || v > 2*time.Second {
		t.Errorf("hash_wait = %v, want ~1 s", d["hash_wait"])
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
		{"timeout with every chunk", "have: 16777216\nspan: 16777216\ndata: 0\n\n", 60, 1500 * time.Millisecond, "timeout", 16 << 20, "99%"},
	}
	// slowSec 2: the watchdog's first tick draws the line, the second one
	// rules.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err, statuses, line := runWarmUp(t, []warmFrame{{0, c.frame}}, false, c.slowSec, c.ctxTimeout)
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
			if line == nil || line.Data["outcome"] != c.reason {
				t.Errorf("measured line %v, want outcome %s", line, c.reason)
			}
		})
	}
}

// The gate's speed, as warmUp computed it before the latch was a type:
// from the moment the counter reached skip bytes; before that, the whole
// warm-up's average (a hard deadline under skip bytes); 0 for nothing.
func TestSpeedLatch(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	var l speedLatch
	if got := l.speed(4<<20, start, start.Add(4*time.Second)); got != 1<<20 {
		t.Errorf("no latch: got %v, want the average over the warm-up", got)
	}
	l.update(1<<20, 2<<20)
	if l.start() != -1 {
		t.Errorf("latched under skip at %d", l.start())
	}
	l.update(3<<20, 2<<20)
	l.update(5<<20, 2<<20)
	if l.start() != 3<<20 {
		t.Errorf("latched at %d, want the first count over skip", l.start())
	}
	at := time.Unix(0, l.ns.Load())
	if got := l.speed(7<<20, start, at.Add(2*time.Second)); got != 2<<20 {
		t.Errorf("latched: got %v, want 4 MiB over 2 s", got)
	}
	if got := l.speed(3<<20, start, at.Add(2*time.Second)); got != 0 {
		t.Errorf("nothing since the latch: got %v", got)
	}
}
