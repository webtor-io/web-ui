package discover

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/webtor-io/web-ui/services/cache_index"
)

type stubLookup struct {
	asked []string
	av    *cache_index.Availability
	err   error
}

func (s *stubLookup) Lookup(_ context.Context, hashes []string) (*cache_index.Availability, error) {
	s.asked = hashes
	return s.av, s.err
}

func ip(i int) *int { return &i }

const (
	hA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hC = "cccccccccccccccccccccccccccccccccccccccc"
)

func TestAvailabilityFor(t *testing.T) {
	// hA: file 2 cached. hB: whole torrent in the Vault. hC: nothing.
	av := cache_index.NewAvailability([]string{hB}, map[string][]int{hA: {2}})
	s := &stubLookup{av: av}
	items := []availabilityItem{
		{InfoHash: hA, FileIdx: ip(2)}, // 0 cached
		{InfoHash: hA, FileIdx: ip(3)}, // 1 another file of it: not
		{InfoHash: hA},                 // 2 no file named: some of it is here
		{InfoHash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", FileIdx: ip(2)}, // 3 addons send either case
		{InfoHash: hB, FileIdx: ip(7)},                                         // 4 vaulted torrent: any file
		{InfoHash: hC, FileIdx: ip(0)},                                         // 5 not
		{InfoHash: "not-a-hash"},                                               // 6 ignored, and never sent to the database
		{InfoHash: hA, FileIdx: ip(-1)},                                        // 7 negative index reads as "not named"
	}
	got, err := availabilityFor(context.Background(), s, items)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{0, 2, 3, 4, 7}; !reflect.DeepEqual(got.Cached, want) {
		t.Errorf("cached = %v, want %v", got.Cached, want)
	}
	if want := []string{hA, hB, hC}; !reflect.DeepEqual(s.asked, want) {
		t.Errorf("asked = %v, want %v (deduplicated, lowercased, valid only)", s.asked, want)
	}
}

func TestAvailabilityForEdges(t *testing.T) {
	// Nothing valid to ask: the index is not called at all.
	s := &stubLookup{}
	got, err := availabilityFor(context.Background(), s, []availabilityItem{{InfoHash: "x"}})
	if err != nil || len(got.Cached) != 0 || s.asked != nil {
		t.Errorf("invalid only: got %v err %v asked %v", got.Cached, err, s.asked)
	}
	// No index configured.
	got, err = availabilityFor(context.Background(), nil, []availabilityItem{{InfoHash: hA}})
	if err != nil || got.Cached == nil || len(got.Cached) != 0 {
		t.Errorf("nil index: got %+v err %v (Cached must be [] for the JSON, not null)", got, err)
	}
	// A failing index is an error for the caller to turn into an empty answer.
	if _, err = availabilityFor(context.Background(), &stubLookup{err: errors.New("db")}, []availabilityItem{{InfoHash: hA}}); err == nil {
		t.Error("want the error back")
	}
}
