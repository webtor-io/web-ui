package resource

import (
	"encoding/base64"
	"testing"

	"github.com/webtor-io/web-ui/services/api"
)

// full512 is a stream's first frame: 512 pieces, the first 64 complete.
func full512() api.EventData {
	var ev api.EventData
	for i := 0; i < 512; i++ {
		ev.Pieces = append(ev.Pieces, testPiece{Position: i, Complete: i < 64})
	}
	return ev
}

func bit(bits []byte, c int) bool { return bits[c/8]&(1<<uint(c%8)) != 0 }

// The seeder's missing runs fold into the bar's cells the way the pieces
// do (512 pieces → 256 cells of two): a cell is hatched when it holds a piece
// nobody connected has and we do not have either.
func TestPieceMap_HolesFoldIntoTheCells(t *testing.T) {
	var m pieceMap
	ev := full512()
	ev.AvailabilityKnown = true
	ev.Missing = []api.PieceRange{{Start: 100, End: 104}, {Start: 511, End: 600}}
	m.apply(ev)
	holes := m.holes()
	if len(holes) != PieceBuckets/8 {
		t.Fatalf("holes: %d bytes, want one bit per cell", len(holes))
	}
	for c := 0; c < PieceBuckets; c++ {
		want := c == 50 || c == 51 || c == 255
		if bit(holes, c) != want {
			t.Errorf("cell %d hatched=%v, want %v", c, bit(holes, c), want)
		}
	}
	// Past the end of the map (run 511..600) is clamped, never grown into.
	if len(m.complete) != 512 {
		t.Errorf("the runs sized the map: %d", len(m.complete))
	}
}

// A run merged past the seeder's 512-run limit also covers pieces that are
// complete here: completion is drawn over the hatch, so a cell whose pieces
// in the run are all complete is not hatched.
func TestPieceMap_CompletionIsDrawnOverTheHatch(t *testing.T) {
	var m pieceMap
	ev := full512()
	ev.AvailabilityKnown = true
	ev.Missing = []api.PieceRange{{Start: 60, End: 66}} // 60..63 complete, 64..65 not
	m.apply(ev)
	holes := m.holes()
	if bit(holes, 30) || bit(holes, 31) {
		t.Error("cells of complete pieces hatched")
	}
	if !bit(holes, 32) {
		t.Error("the cell of the incomplete pieces 64..65 is not hatched")
	}
	// All of a run complete: nothing to hatch, no holes at all.
	ev.Missing = []api.PieceRange{{Start: 0, End: 64}}
	m.apply(ev)
	if h := m.holes(); h != nil {
		t.Errorf("only complete pieces in the runs, yet holes %v", h)
	}
}

// The stream is stateful for missing like for pieces: a frame that says
// missing_unchanged keeps the runs; one that carries missing -- even null or
// [] -- replaces them.
func TestPieceMap_MissingUnchangedKeepsTheRuns(t *testing.T) {
	var m pieceMap
	ev := full512()
	ev.AvailabilityKnown = true
	ev.Missing = []api.PieceRange{{Start: 100, End: 104}}
	m.apply(ev)
	// A piece completed elsewhere; the holes are the same, so left out.
	m.apply(api.EventData{AvailabilityKnown: true, MissingUnchanged: true, Pieces: evWith(testPiece{Position: 5, Complete: true}).Pieces})
	if h := m.holes(); h == nil || !bit(h, 50) {
		t.Fatalf("an unchanged frame dropped the runs: %v", h)
	}
	// A frame without pieces is still a frame: it says the holes are gone.
	m.apply(api.EventData{AvailabilityKnown: true, Missing: []api.PieceRange{}})
	if h := m.holes(); h != nil {
		t.Errorf("[] must clear the runs: %v", h)
	}
	m.apply(api.EventData{AvailabilityKnown: true, Missing: []api.PieceRange{{Start: 300, End: 302}}})
	m.apply(api.EventData{AvailabilityKnown: true})
	if h := m.holes(); h != nil {
		t.Errorf("null must clear the runs: %v", h)
	}
}

// Until the seeder knows the peers' piece sets its union is a lower bound
// and early holes are not there: nothing is hatched, and nothing is missing
// (a seeder without the fields reads the same way).
func TestPieceMap_HolesNeedAvailabilityKnown(t *testing.T) {
	var m pieceMap
	ev := full512()
	ev.Missing = []api.PieceRange{{Start: 100, End: 104}}
	m.apply(ev)
	if h := m.holes(); h != nil {
		t.Errorf("availability not known, yet holes %v", h)
	}
	ev.AvailabilityKnown = true
	m.apply(ev)
	if h := m.holes(); h == nil {
		t.Fatal("known now: the holes")
	}
	m.apply(api.EventData{MissingUnchanged: true})
	if h := m.holes(); h != nil {
		t.Errorf("the next frame not known again, yet holes %v", h)
	}
}

// The holes ride on the status next to the pieces, only while the bar is
// drawn, with the seeder's availability for the view.
func TestResolveStatus_CarriesTheHoles(t *testing.T) {
	stats := &TorrentStatsData{Total: 100, Completed: 10, Peers: 12, Fill: []byte{255, 0}, Active: []byte{0},
		Holes: []byte{2}, AvailabilityKnown: true, Availability: 0.73, WantedMissing: 3, ReaderMissing: 1}
	st := resolveStatus(nil, nil, stats)
	if st.State != "caching" || st.Missing != base64.StdEncoding.EncodeToString([]byte{2}) {
		t.Fatalf("caching: %+v", st)
	}
	tr := st.viewTorrent(false)
	if !tr.AvailabilityKnown || tr.Availability != 0.73 || !tr.Missing || tr.WantedMissing != 3 || tr.ReaderMissing != 1 || tr.Peers != 12 {
		t.Errorf("view torrent: %+v", tr)
	}
	stats.Completed = 100
	if cached := resolveStatus(nil, nil, stats); cached.State != "cached" || cached.Missing != "" {
		t.Errorf("no bar, no holes: %+v", cached)
	}
}

// Until the seeder knows the peers' pieces nothing of its availability
// reaches the view -- neither the hatch nor the facts the missing states
// are picked by: its union is a lower bound, holes that are not there.
func TestResolveStatus_AvailabilityNotKnownIsNotRead(t *testing.T) {
	stats := &TorrentStatsData{Total: 100, Completed: 10, Peers: 12, Fill: []byte{255, 0}, Active: []byte{0},
		Holes: []byte{2}, Availability: 0.73, WantedMissing: 3, ReaderMissing: 1}
	st := resolveStatus(nil, nil, stats)
	if st.Missing != "" {
		t.Errorf("hatched while not known: %q", st.Missing)
	}
	if tr := st.viewTorrent(false); tr.AvailabilityKnown || tr.Missing || tr.WantedMissing != 0 || tr.ReaderMissing != 0 || tr.Availability != 0 {
		t.Errorf("view torrent read the availability: %+v", tr)
	}
}
