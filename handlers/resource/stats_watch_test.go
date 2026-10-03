package resource

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/api"
)

// newTestStatsWatch: every connect succeeds with a fresh stream the test
// never writes to (frames go in through frame), counted in dials; jitter
// pinned to the middle of its range (factor 1).
func newTestStatsWatch() (*statsWatch, *fakeClock, *atomic.Int32) {
	clk := &fakeClock{}
	dials := &atomic.Int32{}
	w := newStatsWatch("res", func(context.Context) statsConn {
		dials.Add(1)
		return statsConn{ch: make(chan api.EventData), msg: "connected"}
	}, clk.after)
	w.jitter = func() float64 { return 0.5 }
	return w, clk, dials
}

// recvStats waits for the watch's async connect to report.
func recvStats(t *testing.T, w *statsWatch) statsConn {
	t.Helper()
	select {
	case r := <-w.results:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("connect did not report")
		return statsConn{}
	}
}

// statsEvent is a live frame of a 100 MiB torrent with done MiB stored.
func statsEvent(done, seeders, peers int) api.EventData {
	return api.EventData{Total: 100 << 20, Completed: done << 20, Seeders: seeders, Peers: peers}
}

// A stream that closes mid-download is reopened after the session
// stream's backoff -- jittered, so a seeder rollout's streams do not all
// come back at once.
func TestStatsWatch_ReconnectUsesTheJitteredBackoff(t *testing.T) {
	ctx := context.Background()
	w, clk, _ := newTestStatsWatch()
	w.dial(ctx)
	t0 := time.Now()
	w.result(ctx, recvStats(t, w), t0)
	w.frame(statsEvent(40, 3, 5), t0)
	w.frame(statsEvent(44, 3, 5), t0.Add(time.Second))
	w.closed(ctx, t0.Add(2*time.Second))
	if len(clk.delays) != 1 || clk.delays[0] != retryDelay(1, 0.5) {
		t.Fatalf("delays %v, want [%v]", clk.delays, retryDelay(1, 0.5))
	}
	w.jitter = func() float64 { return 0 }
	clk.fire()
	w.result(ctx, recvStats(t, w), t0.Add(4*time.Second))
	w.frame(statsEvent(48, 3, 5), t0.Add(5*time.Second))
	w.closed(ctx, t0.Add(6*time.Second))
	if len(clk.delays) != 2 || clk.delays[1] != retryDelay(2, 0) {
		t.Fatalf("delays %v, want the second at %v", clk.delays, retryDelay(2, 0))
	}
}

// A reconnect due after the status stream ended dials nothing: the tab is
// gone (this used to log a "context canceled" Warn for every one).
func TestStatsWatch_NoDialAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w, clk, dials := newTestStatsWatch()
	w.dial(ctx)
	t0 := time.Now()
	w.result(ctx, recvStats(t, w), t0)
	w.frame(statsEvent(40, 3, 5), t0)
	w.frame(statsEvent(44, 3, 5), t0.Add(time.Second))
	w.closed(ctx, t0.Add(2*time.Second))
	if clk.pending() != 1 {
		t.Fatalf("pending %d, want the reconnect", clk.pending())
	}
	cancel()
	clk.fire()
	time.Sleep(50 * time.Millisecond)
	if n := dials.Load(); n != 1 {
		t.Errorf("%d connects, want only the first", n)
	}
}

// The seeder ends every stats stream at 30 minutes (torrent-web-seeder
// Stat.StatStream). Each of those closes used to cost a reconnect from a
// budget of five that never came back, and was only reopened with progress
// in the last minute: the sixth close of a download -- or the first of a
// paused one -- left the status idle for as long as the tab stayed open.
// A stream that lived is reopened whatever the progress, and its close
// gives the budget back.
func TestStatsWatch_PlannedClosesKeepTheStatus(t *testing.T) {
	for _, c := range []struct {
		name string
		// moving: progress right before every close; otherwise the
		// reader stopped at 43% long ago.
		moving bool
		want   string
	}{
		{"downloading", true, "caching"},
		{"paused", false, "caching"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w, clk, dials := newTestStatsWatch()
			w.dial(ctx)
			now := time.Now()
			w.result(ctx, recvStats(t, w), now)
			done := 40
			w.frame(statsEvent(done, 3, 5), now)
			done += 3
			w.frame(statsEvent(done, 3, 5), now.Add(time.Second))
			for i := 1; i <= 6; i++ {
				opened := now
				if c.moving {
					done++
					w.frame(statsEvent(done, 3, 5), opened.Add(30*time.Minute-time.Second))
				}
				now = opened.Add(30 * time.Minute)
				w.closed(ctx, now)
				if st := w.status(nil, nil, now); st.State != c.want {
					t.Fatalf("close %d: %s, want %s", i, st.State, c.want)
				}
				if clk.pending() != 1 {
					t.Fatalf("close %d: no reconnect scheduled (%d connects so far)", i, dials.Load())
				}
				clk.fire()
				now = now.Add(2 * time.Second)
				w.result(ctx, recvStats(t, w), now)
				// A new stream opens with the full frame.
				w.frame(statsEvent(done, 3, 5), now)
				if st := w.status(nil, nil, now.Add(time.Second)); st.State != c.want {
					t.Fatalf("reopened %d: %s, want %s", i, st.State, c.want)
				}
			}
			if n := dials.Load(); n != 7 {
				t.Errorf("%d connects, want 7", n)
			}
		})
	}
}

// What stays as it was: a stream that dies young on a torrent nobody moved
// is not reopened, nor is one of a complete torrent (the seeder closes it
// at 100%).
func TestStatsWatch_ShortStreamWithoutProgressIsNotReopened(t *testing.T) {
	ctx := context.Background()
	w, clk, _ := newTestStatsWatch()
	w.dial(ctx)
	t0 := time.Now()
	w.result(ctx, recvStats(t, w), t0)
	w.frame(statsEvent(40, 3, 5), t0)
	w.closed(ctx, t0.Add(10*time.Second))
	if clk.pending() != 0 || w.last != nil {
		t.Errorf("young stream, no progress: pending %d, last %v", clk.pending(), w.last)
	}
	w, clk, _ = newTestStatsWatch()
	w.dial(ctx)
	w.result(ctx, recvStats(t, w), t0)
	w.frame(statsEvent(100, 3, 5), t0)
	w.closed(ctx, t0.Add(30*time.Minute))
	if clk.pending() != 0 || w.status(nil, nil, t0.Add(30*time.Minute)).State != "cached" {
		t.Errorf("complete torrent: pending %d", clk.pending())
	}
}
