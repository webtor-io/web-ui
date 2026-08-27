package memwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWatch(t *testing.T, heap *uint64) (*Watch, *int) {
	t.Helper()
	dumps := 0
	w := &Watch{
		threshold: 100,
		cooldown:  10 * time.Minute,
		keep:      2,
		dir:       t.TempDir(),
		readHeap:  func() uint64 { return *heap },
	}
	w.dump = func(now time.Time) error { dumps++; return nil }
	return w, &dumps
}

func TestMaybeDumpThresholdAndCooldown(t *testing.T) {
	heap := uint64(50)
	w, dumps := newTestWatch(t, &heap)
	now := time.Unix(1000, 0)

	w.maybeDump(now)
	if *dumps != 0 {
		t.Fatalf("dumped below threshold")
	}

	heap = 150
	w.maybeDump(now)
	if *dumps != 1 {
		t.Fatalf("no dump above threshold, got %d", *dumps)
	}

	// still above threshold, inside cooldown — must not dump again
	w.maybeDump(now.Add(time.Minute))
	if *dumps != 1 {
		t.Fatalf("dumped inside cooldown, got %d", *dumps)
	}

	// past cooldown, still above — dump again
	w.maybeDump(now.Add(11 * time.Minute))
	if *dumps != 2 {
		t.Fatalf("no dump after cooldown, got %d", *dumps)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	heap := uint64(0)
	w, _ := newTestWatch(t, &heap)
	names := []string{
		"heap-20260101-000000.pb.gz",
		"heap-20260102-000000.pb.gz",
		"heap-20260103-000000.pb.gz",
		"goroutine-20260101-000000.pb.gz",
		"goroutine-20260102-000000.pb.gz",
		"goroutine-20260103-000000.pb.gz",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(w.dir, n), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	w.prune()
	left, err := filepath.Glob(filepath.Join(w.dir, "*.pb.gz"))
	if err != nil {
		t.Fatal(err)
	}
	// keep=2 → newest two timestamps survive, for both profile kinds
	want := map[string]bool{
		"heap-20260102-000000.pb.gz":      true,
		"heap-20260103-000000.pb.gz":      true,
		"goroutine-20260102-000000.pb.gz": true,
		"goroutine-20260103-000000.pb.gz": true,
	}
	if len(left) != len(want) {
		t.Fatalf("got %d files left, want %d: %v", len(left), len(want), left)
	}
	for _, p := range left {
		if !want[filepath.Base(p)] {
			t.Errorf("unexpected survivor %s", filepath.Base(p))
		}
	}
}

// The real dumper must produce both profile files and prune old ones.
func TestWriteProfiles(t *testing.T) {
	heap := uint64(0)
	w, _ := newTestWatch(t, &heap)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if err := w.writeProfiles(now); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"heap", "goroutine"} {
		p := filepath.Join(w.dir, kind+"-20260827-120000.pb.gz")
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s profile missing: %v", kind, err)
		}
		if st.Size() == 0 {
			t.Fatalf("%s profile empty", kind)
		}
	}
}
