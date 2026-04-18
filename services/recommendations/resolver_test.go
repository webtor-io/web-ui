package recommendations

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/webtor-io/web-ui/models"
)

// fakeLookup is a programmable MetadataLookup for tests. For each (title,
// year) it returns either a pre-canned VideoMetadata, a nil (not found),
// or an error. It also counts total calls to assert concurrency budgets.
type fakeLookup struct {
	data   map[string]*models.VideoMetadata
	errs   map[string]error
	calls  int64
	onCall func()
}

func (f *fakeLookup) LookupByTitleYear(ctx context.Context, title string, year *int16, ct models.ContentType) (*models.VideoMetadata, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.onCall != nil {
		f.onCall()
	}
	if err, ok := f.errs[title]; ok {
		return nil, err
	}
	return f.data[title], nil
}

func ratingPtr(v float64) *float64 { y := v; return &y }
func yearPtr(y int16) *int16       { return &y }

func TestResolver_DropsNonImdbAndUnresolved(t *testing.T) {
	f := &fakeLookup{
		data: map[string]*models.VideoMetadata{
			"Interstellar": {VideoID: "tt0816692", Title: "Interstellar", Year: yearPtr(2014), PosterURL: "p1", Rating: ratingPtr(8.6)},
			"Arrival":      {VideoID: "tt2543164", Title: "Arrival", Year: yearPtr(2016), Rating: ratingPtr(7.9)},
			"Weird Thing":  {VideoID: "tmdb99999", Title: "Weird Thing"}, // non-IMDB, must be dropped
			// "Made Up Movie" intentionally missing → nil lookup → drop
		},
		errs: map[string]error{
			"Boom": errors.New("tmdb unavailable"),
		},
	}
	r := NewResolver(f, nil,3)

	items := []claudeItem{
		{Title: "Interstellar", Year: 2014, Reason: "you loved Tenet"},
		{Title: "Made Up Movie", Year: 2099, Reason: "invented"},
		{Title: "Arrival", Year: 2016, Reason: "more cerebral sci-fi"},
		{Title: "Weird Thing", Reason: "tmdb fallback"},
		{Title: "Boom", Reason: "upstream error"},
	}

	got := r.Resolve(context.Background(),items, models.ContentTypeMovie, "")

	if len(got) != 2 {
		t.Fatalf("want 2 resolved, got %d: %+v", len(got), got)
	}
	// Order must match input order: Interstellar first, Arrival second.
	if got[0].VideoID != "tt0816692" {
		t.Errorf("want Interstellar first, got %s", got[0].VideoID)
	}
	if got[1].VideoID != "tt2543164" {
		t.Errorf("want Arrival second, got %s", got[1].VideoID)
	}
	// Reasons must be copied across unchanged.
	if got[0].Reason != "you loved Tenet" {
		t.Errorf("reason dropped: %q", got[0].Reason)
	}
	if got[0].Rating != 8.6 {
		t.Errorf("rating dropped: %v", got[0].Rating)
	}
	if got[0].Type != "movie" {
		t.Errorf("type wrong: %q", got[0].Type)
	}
}

func TestResolver_SeriesType(t *testing.T) {
	f := &fakeLookup{
		data: map[string]*models.VideoMetadata{
			"Breaking Bad": {VideoID: "tt0903747", Title: "Breaking Bad", Year: yearPtr(2008)},
		},
	}
	r := NewResolver(f, nil,1)

	got := r.Resolve(context.Background(),[]claudeItem{{Title: "Breaking Bad", Year: 2008, Reason: "obvious"}}, models.ContentTypeSeries, "")
	if len(got) != 1 || got[0].Type != "series" {
		t.Fatalf("expected series type, got %+v", got)
	}
}

func TestResolver_EmptyInput(t *testing.T) {
	r := NewResolver(&fakeLookup{}, nil, 2)
	if got := r.Resolve(context.Background(),nil, models.ContentTypeMovie, ""); len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestResolver_ConcurrencyLimit(t *testing.T) {
	// Track max-in-flight via a channel-gated counter.
	var inflight, max int64
	gate := make(chan struct{})
	f := &fakeLookup{
		data: map[string]*models.VideoMetadata{},
		onCall: func() {
			cur := atomic.AddInt64(&inflight, 1)
			for {
				m := atomic.LoadInt64(&max)
				if cur <= m || atomic.CompareAndSwapInt64(&max, m, cur) {
					break
				}
			}
			<-gate
			atomic.AddInt64(&inflight, -1)
		},
	}
	r := NewResolver(f, nil,3)

	items := make([]claudeItem, 10)
	for i := range items {
		items[i] = claudeItem{Title: "missing"}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Resolve(context.Background(),items, models.ContentTypeMovie, "")
	}()

	// Let things run briefly to saturate the semaphore, then release.
	for i := 0; i < 10; i++ {
		gate <- struct{}{}
	}
	<-done

	if atomic.LoadInt64(&max) > 3 {
		t.Fatalf("concurrency exceeded 3: peak=%d", max)
	}
	if f.calls != 10 {
		t.Fatalf("expected 10 calls, got %d", f.calls)
	}
}
