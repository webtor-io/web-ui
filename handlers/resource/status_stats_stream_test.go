package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/statusview"
)

// The seeder's stats stream through the real status handler and statusLoop:
// a frame of the seeder as it is sent, and the status message the page
// gets for it. The helpers (pieceMap, resolveStatus, statusview) have their
// own tests; this is the wiring between them, which no other test crosses --
// dropping the availability from the loop's TorrentStatsData left every
// other test green while the page lost the hatch and all three
// missing-piece states.

// statsFrameJSON is one statupdate frame: a torrent of n pieces of 1 MiB
// with the first done complete, and the swarm and availability fields as
// given (extra is spliced in as it is: "" for none).
func statsFrameJSON(n, done, peers, seeders int, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"total":%d,"completed":%d,"peers":%d,"seeders":%d,"live":true,"pieces":[`, n<<20, done<<20, peers, seeders)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"position":%d,"complete":%v,"priority":0}`, i, i < done)
	}
	b.WriteString("]")
	if extra != "" {
		b.WriteString("," + extra)
	}
	b.WriteString("}")
	return b.String()
}

// A swarm with 12 peers and no seeder, pieces 100-139 of 512 on nobody's
// side: the hatch and missing_idle, kept across a frame that says the runs
// did not change, and gone once a frame says there are none.
func TestStatusStream_AvailabilityEndToEnd(t *testing.T) {
	const avail = `"availability":0.73,"availability_known":true,"wanted_missing":5,"reader_missing":0`
	node := newFakeNode(t, nil, func(n *fakeNode) {
		n.stats = []statFrame{
			{0, statsFrameJSON(512, 40, 12, 0, avail+`,"missing":[{"start":100,"end":140}],"missing_unchanged":false`)},
			// The runs did not change: the frame leaves them out.
			{time.Second, `{"total":536870912,"completed":41943040,"peers":12,"seeders":0,"live":true,` + avail + `,"missing":null,"missing_unchanged":true}`},
			// Now there are none, and nothing wanted is missing.
			{settleAfter + 2*time.Second, `{"total":536870912,"completed":41943040,"peers":12,"seeders":0,"live":true,` +
				`"availability":1,"availability_known":true,"wanted_missing":0,"reader_missing":0,"missing":null,"missing_unchanged":false}`},
		}
	})
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	// Parallel only past the setup: statusServer's i18n.New writes the
	// package's globals.
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	// While the stream settles nothing is blamed on the missing pieces:
	// the first seconds of a swarm that downloads look the same.
	m := until(t, msgs, 5*time.Second, "the first caching status", func(m map[string]any) bool { return m["state"] == "caching" })
	if k := get(m, "view", "key"); k == statusview.KeyMissingIdle {
		t.Errorf("settling, yet missing_idle: %v", m["view"])
	}
	// Settled, past the frame that left the runs out: missing_idle, the
	// hatch where pieces 100-139 are (512 pieces fold into 256 cells, two
	// a cell: cells 50-69), and Vault offered.
	m = until(t, msgs, settleAfter+2*time.Second, "missing_idle", func(m map[string]any) bool {
		return get(m, "view", "key") == statusview.KeyMissingIdle
	})
	if get(m, "view", "mode") != statusview.ModeBadge || get(m, "view", "vault") != true {
		t.Errorf("missing_idle: %v", m["view"])
	}
	holes, _ := m["missing"].(string)
	if holes == "" {
		t.Fatalf("no hatch on missing_idle: %v", m)
	}
	cells := decodeBits(t, holes)
	for c := 0; c < 256; c++ {
		if want := c >= 50 && c < 70; cells[c] != want {
			t.Fatalf("cell %d hatched %v: %v", c, cells[c], cells)
		}
	}
	if hint, _ := get(m, "view", "hint").(string); !strings.Contains(hint, "73%") {
		t.Errorf("hint %q", hint)
	}
	// "none": the hatch goes, and with nothing wanted missing so does the
	// story -- the swarm is idle, and paused.
	m = until(t, msgs, 5*time.Second, "the hatch gone", func(m map[string]any) bool { return m["missing"] == nil })
	if k := get(m, "view", "key"); k == statusview.KeyMissingIdle || get(m, "view", "vault") == true {
		t.Errorf("no holes left, yet %v", m["view"])
	}
}

// decodeBits reads a status bitset (base64, bit i of byte i/8).
func decodeBits(t *testing.T, b64 string) []bool {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]bool, len(b)*8)
	for i := range out {
		out[i] = b[i/8]&(1<<uint(i%8)) != 0
	}
	return out
}

// The swarm sends a piece a second for five seconds, then stops. The chain
// shows it moving -- a speed, the sweep -- until HoldFor after the last
// piece, drawn as it last moved all along (never a pause or a dash), and
// the badge follows about ten seconds after the last piece, not after the
// smoothed rate's tail has died out on top of that (14-22 s before). The
// swarm has pieces nobody connected has: the first seconds, before the
// first piece, are not "needed pieces missing" either.
func TestStatusStream_BadgeTenSecondsAfterTheLastPiece(t *testing.T) {
	const avail = `"availability":0.9,"availability_known":true,"wanted_missing":0,"reader_missing":0`
	frames := []statFrame{{0, statsFrameJSON(64, 8, 12, 0, avail+`,"missing":[{"start":60,"end":64}],"missing_unchanged":false`)}}
	const pieces = 5
	for i := 1; i <= pieces; i++ {
		frames = append(frames, statFrame{time.Duration(i) * time.Second, fmt.Sprintf(
			`{"total":%d,"completed":%d,"peers":12,"seeders":0,"live":true,"pieces":[{"position":%d,"complete":true,"priority":0}],%s,"missing_unchanged":true}`,
			64<<20, (8+4*i)<<20, 8+i, avail)})
	}
	node := newFakeNode(t, nil, func(n *fakeNode) { n.stats = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	// Parallel only past the setup: statusServer's i18n.New writes the
	// package's globals.
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	lastPiece := time.Duration(pieces) * time.Second
	chained := false
	for {
		var m map[string]any
		select {
		case m = <-msgs:
		case <-ctx.Done():
			t.Fatal("no badge after the swarm stopped")
		}
		if m == nil || m["view"] == nil {
			continue
		}
		at := time.Since(start)
		key, mode := get(m, "view", "key"), get(m, "view", "mode")
		speed, _ := get(m, "view", "segs", 0, "speed").(string)
		if mode == statusview.ModeChain {
			chained = true
			if strings.Contains(speed, "—") || strings.Contains(speed, "пауза") || strings.Contains(speed, "paused") || get(m, "view", "segs", 0, "on") != true {
				t.Errorf("+%v: the chain's swarm is not drawn as it last moved: %v", at.Round(time.Millisecond), get(m, "view", "segs", 0))
			}
			continue
		}
		if !chained {
			if key == statusview.KeyMissingIdle {
				t.Errorf("+%v: missing_idle before the swarm's first piece", at.Round(time.Millisecond))
			}
			continue
		}
		// The badge. The frames leave the fake a little after their
		// time; the loop sees the last piece then, and gives the swarm up
		// HoldFor later, at its next tick.
		after := at - lastPiece
		t.Logf("badge %v after the last piece: %v", after.Round(time.Millisecond), key)
		if after < statusview.HoldFor-500*time.Millisecond || after > statusview.HoldFor+2500*time.Millisecond {
			t.Errorf("the badge %v after the last piece, want about %v", after.Round(time.Millisecond), statusview.HoldFor)
		}
		if key != statusview.KeyMissingIdle {
			t.Errorf("the badge after the hold: %v, want missing_idle", key)
		}
		return
	}
}

// The seeder closes the stats stream once the torrent is complete. The loop
// does not reopen it (shouldReconnect: nothing left to download), and it
// used to forget the stats with it -- the complete torrent read "idle", the
// chain's "Webtor ожидает" and the badge's "Ожидает" instead of "В кэше"
// (5461f58a…, 2026-09-25 12:33:24Z). Complete is complete: the status
// stays cached after the stream is gone.
func TestStatusStream_CompleteTorrentStaysCachedWhenTheStreamCloses(t *testing.T) {
	const n = 64
	last := make([]string, 0, 4)
	for p := 60; p < n; p++ {
		last = append(last, fmt.Sprintf(`{"position":%d,"complete":true,"priority":0}`, p))
	}
	node := newFakeNode(t, nil, func(f *fakeNode) {
		f.stats = []statFrame{
			{0, statsFrameJSON(n, 60, 3, 3, "")},
			{time.Second, fmt.Sprintf(`{"total":%d,"completed":%d,"peers":3,"seeders":3,"live":true,"pieces":[%s]}`, n<<20, n<<20, strings.Join(last, ","))},
		}
		f.statsEnd = true
	})
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	until(t, msgs, 5*time.Second, "caching", func(m map[string]any) bool { return m["state"] == "caching" })
	m := until(t, msgs, 5*time.Second, "cached", func(m map[string]any) bool { return m["state"] == "cached" })
	if get(m, "view", "key") != statusview.KeyCached || get(m, "view", "badge", "icon") != "check" {
		t.Errorf("complete: %v", m["view"])
	}
	// The stream is gone now (or is about to be): whatever the loop says
	// from here on is still the complete torrent.
	deadline := time.After(4 * time.Second)
	for {
		select {
		case m = <-msgs:
			if m == nil {
				t.Fatal("the status stream ended")
			}
			if m["state"] != "cached" || get(m, "view", "key") != statusview.KeyCached {
				t.Fatalf("after the seeder closed the stream (%v ago): %v %v", time.Since(node.statsEnded()).Round(time.Millisecond), m["state"], get(m, "view", "key"))
			}
		case <-deadline:
			if node.statsEnded().IsZero() {
				t.Fatal("the seeder's stream never ended")
			}
			return
		}
	}
}

// seederTerminatedFrame is the frame a seeder pod sends on every stats
// stream it holds when it gets SIGTERM (torrent-web-seeder StatStream:
// StatReply{Status: TERMINATED}, JSON as its web handler writes it): every
// counter zero.
const seederTerminatedFrame = `{"total":0,"completed":0,"peers":0,"status":3,"pieces":null,"seeders":0,"leechers":0,"live":false,"availability":0,"availability_known":false,"missing":null,"missing_unchanged":false,"wanted_missing":0,"reader_missing":0}`

// A deploy rotates the seeder pods mid-download. A pod on its way out sends
// its TERMINATED frame and ends the stream. Taken for stats, that frame was
// a torrent with nothing stored: the page read "idle" ("Ожидает"), and
// the close that followed was never reconnected (shouldReconnect wants
// something stored) -- the page never learnt the torrent finished on the
// new pod. The frame is not stats: the download's status stays on screen
// through the gap, the stream is reopened, and the torrent the new pod
// finishes reads cached.
func TestStatusStream_SeederRolloutMidDownloadReconnects(t *testing.T) {
	const n = 64
	piece := func(done int) string {
		return fmt.Sprintf(`{"total":%d,"completed":%d,"peers":5,"seeders":2,"live":true,"pieces":[{"position":%d,"complete":true,"priority":0}]}`, n<<20, done<<20, done-1)
	}
	node := newFakeNode(t, nil, func(f *fakeNode) {
		f.stats = []statFrame{
			{0, statsFrameJSON(n, 40, 5, 2, "")},
			{time.Second, piece(48)},
			{2 * time.Second, piece(52)},
			{2500 * time.Millisecond, seederTerminatedFrame},
		}
		// The new pod has the whole torrent: it says so, and closes the
		// stream as a complete torrent's.
		f.statsNext = []statFrame{{0, statsFrameJSON(n, n, 5, 2, "")}}
		f.statsEnd = true
	})
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	until(t, msgs, 5*time.Second, "caching", func(m map[string]any) bool { return m["state"] == "caching" })
	deadline := time.After(12 * time.Second)
	for {
		select {
		case m := <-msgs:
			if m == nil {
				t.Fatal("the status stream ended")
			}
			at := time.Since(start).Round(time.Millisecond)
			switch m["state"] {
			case "caching":
				continue
			case "cached":
				if get(m, "view", "key") != statusview.KeyCached {
					t.Errorf("+%v cached: %v", at, m["view"])
				}
				if o := node.statOpens.Load(); o < 2 {
					t.Errorf("cached with %d stats stream(s): not from the new pod", o)
				}
				return
			default:
				t.Fatalf("+%v mid-download, the seeder rolled out: %v %v", at, m["state"], get(m, "view", "key"))
			}
		case <-deadline:
			t.Fatalf("never cached: %d stats stream(s) opened", node.statOpens.Load())
		}
	}
}
