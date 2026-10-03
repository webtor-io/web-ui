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
