package resource

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/statusview"
)

// The seeder's own stats stream, byte for byte, through the real status
// handler: testdata/seeder_statstream_availability.sse is torrent-web-seeder's
// golden file (server/services/testdata/statstream_availability.sse, the sha
// on its first line), real replies sent the way StatStream sends them. The
// other tests here parse hand-written JSON, so a key the seeder renamed, or
// a missing_unchanged it changed, would leave them green; this one goes red.
// After a deliberate change on the seeder's side, copy its file over and
// keep the first line.

// seederGolden is the golden file's frames by the SSE comment naming them.
func seederGolden(t *testing.T) func(note string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/seeder_statstream_availability.sse")
	if err != nil {
		t.Fatal(err)
	}
	frames, note := map[string]string{}, ""
	for _, l := range strings.Split(string(b), "\n") {
		if n, ok := strings.CutPrefix(l, ": "); ok {
			note = n
		} else if d, ok := strings.CutPrefix(l, "data: "); ok {
			frames[note] = d
		}
	}
	return func(note string) string {
		t.Helper()
		d, ok := frames[note]
		if !ok {
			t.Fatalf("no frame %q in the seeder's golden file", note)
		}
		return d
	}
}

// goldenStream opens the page's status stream against a seeder sending
// stats and a thp sending sess.
func goldenStream(t *testing.T, stats, sess []statFrame) <-chan map[string]any {
	t.Helper()
	node := newFakeNode(t, nil, func(n *fakeNode) { n.stats, n.sessFrames = stats, sess })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	// Parallel only past the setup: statusServer's i18n.New writes the
	// package's globals.
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	return msgs
}

func isMissingKey(k any) bool {
	return k == statusview.KeyMissing || k == statusview.KeyMissingIdle || k == statusview.KeyVaultMissing
}

// The golden torrent: 9 pieces, one cell each; pieces 4-8 on nobody's side.
func checkGoldenHatch(t *testing.T, m map[string]any) {
	t.Helper()
	holes, _ := m["missing"].(string)
	if holes == "" {
		t.Fatalf("no hatch: %v", m)
	}
	for c, hatched := range decodeBits(t, holes) {
		if want := c >= 4 && c < 9; hatched != want {
			t.Errorf("cell %d hatched %v, want %v", c, hatched, want)
		}
	}
}

// availability_known false: its 44% is a lower bound and its holes are not
// there -- no hatch and none of the missing states, also past settling. The
// seeder sends no runs and no counts then; web-ui's own gate on the flag,
// for a frame that would, is TestPieceMap_HolesNeedAvailabilityKnown's and
// TestResolveStatus_AvailabilityNotKnownIsNotRead's.
func TestSeederGolden_NotKnownIsNotBlamed(t *testing.T) {
	frame := seederGolden(t)
	msgs := goldenStream(t, []statFrame{{0, frame("root, peers connected under 20 s")}}, nil)
	end := time.After(settleAfter + 3*time.Second)
	n := 0
	for {
		select {
		case m, open := <-msgs:
			if !open {
				t.Fatal("stream ended")
			}
			if m["view"] == nil {
				continue
			}
			n++
			if k := get(m, "view", "key"); isMissingKey(k) || m["missing"] != nil {
				t.Errorf("availability not known, yet key %v, missing %v", k, m["missing"])
			}
		case <-end:
			if n == 0 {
				t.Fatal("no status")
			}
			return
		}
	}
}

// Settled: missing_idle with the hatch where pieces 4-8 are and the share
// in the hint; the next frame leaves the runs out (missing_unchanged) and
// the hatch stays; the one after says there are none, and it goes.
func TestSeederGolden_HolesComeAndGo(t *testing.T) {
	frame := seederGolden(t)
	msgs := goldenStream(t, []statFrame{
		{0, frame("root, settled")},
		{settleAfter + 3*time.Second, frame("next frame: piece 0 completed, holes the same")},
		{settleAfter + 5*time.Second, frame("next frame: a seeder connected, nothing missing")},
	}, nil)

	m := until(t, msgs, settleAfter+3*time.Second, "missing_idle", func(m map[string]any) bool {
		return get(m, "view", "key") == statusview.KeyMissingIdle
	})
	if get(m, "view", "bar", "mode") != "pieces" {
		t.Errorf("missing_idle without the piece bar: %v", m["view"])
	}
	checkGoldenHatch(t, m)
	if hint, _ := get(m, "view", "hint").(string); !strings.Contains(hint, "44%") {
		t.Errorf("hint %q", hint)
	}

	m = until(t, msgs, 5*time.Second, "piece 0 complete", func(m map[string]any) bool {
		p, _ := m["pieces"].(string)
		b, _ := base64.StdEncoding.DecodeString(p)
		return len(b) > 0 && b[0] == 255
	})
	checkGoldenHatch(t, m)

	m = until(t, msgs, 5*time.Second, "the hatch gone", func(m map[string]any) bool { return m["missing"] == nil })
	if k := get(m, "view", "key"); isMissingKey(k) || get(m, "view", "bar", "mode") != "pieces" {
		t.Errorf("no holes left: key %v, bar %v", k, get(m, "view", "bar"))
	}
}

// A reader blocked on piece 8, which nobody connected has, and the viewer's
// request open with nothing coming: missing, the wait blamed on the piece.
func TestSeederGolden_ReaderMissing(t *testing.T) {
	frame := seederGolden(t)
	waiting := make([]held, 15)
	for i := range waiting {
		waiting[i] = held{0, 1}
	}
	msgs := goldenStream(t, []statFrame{{0, frame("root, settled, a reader blocked on piece 8")}}, sessEvents(heldEvents(waiting...)...))
	m := until(t, msgs, 12*time.Second, "missing", func(m map[string]any) bool {
		return get(m, "view", "key") == statusview.KeyMissing
	})
	checkGoldenHatch(t, m)
	if hint, _ := get(m, "view", "hint").(string); !strings.Contains(hint, "44%") {
		t.Errorf("hint %q", hint)
	}
}
