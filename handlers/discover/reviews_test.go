package discover

import (
	"context"
	"errors"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

type mockReviewsEnricher struct {
	byID  map[string][]enrich.Review
	errID map[string]bool
	calls []struct {
		id string
		ct models.ContentType
	}
}

func (m *mockReviewsEnricher) HasMappers() bool { return true }

func (m *mockReviewsEnricher) ReviewsByID(_ context.Context, videoID string, ct models.ContentType) ([]enrich.Review, error) {
	m.calls = append(m.calls, struct {
		id string
		ct models.ContentType
	}{videoID, ct})
	if m.errID[videoID] {
		return nil, errors.New("tmdb timeout")
	}
	return m.byID[videoID], nil
}

func TestReviewsForID_MapsFields(t *testing.T) {
	rating := 8.0
	en := &mockReviewsEnricher{byID: map[string][]enrich.Review{
		"tt1": {{Author: "alice", Rating: &rating, Content: "great", URL: "https://tmdb/r/1", CreatedAt: "2024-01-01T00:00:00Z"}},
	}}
	got, err := reviewsForID(context.Background(), en, "tt1", "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 review, got %d", len(got))
	}
	r := got[0]
	if r.Author != "alice" || r.Content != "great" || r.URL != "https://tmdb/r/1" || r.CreatedAt != "2024-01-01T00:00:00Z" {
		t.Errorf("review mismatch: %+v", r)
	}
	if r.Rating == nil || *r.Rating != 8.0 {
		t.Errorf("rating mismatch: %+v", r.Rating)
	}
	if en.calls[0].ct != models.ContentTypeSeries {
		t.Errorf("expected series content type, got %v", en.calls[0].ct)
	}
}

func TestReviewsForID_NoneIsEmptyNotError(t *testing.T) {
	en := &mockReviewsEnricher{byID: map[string][]enrich.Review{}}
	got, err := reviewsForID(context.Background(), en, "tt-none", "movie")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil slice (definitive no-reviews verdict), got %#v", got)
	}
	if en.calls[0].ct != models.ContentTypeMovie {
		t.Errorf("expected movie content type for non-series hint, got %v", en.calls[0].ct)
	}
}

func TestReviewsForID_ErrorPropagates(t *testing.T) {
	en := &mockReviewsEnricher{errID: map[string]bool{"tt-flaky": true}}
	_, err := reviewsForID(context.Background(), en, "tt-flaky", "movie")
	if err == nil {
		t.Fatal("expected pipeline error to propagate (client must not cache the miss)")
	}
}
