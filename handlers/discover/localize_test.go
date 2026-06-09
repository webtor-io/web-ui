package discover

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/webtor-io/web-ui/models"
)

type mockEnricher struct {
	mu    sync.Mutex
	calls []string
	byID  map[string][2]string // id -> {title, plot}
	errID map[string]bool      // id -> pipeline error
}

func (m *mockEnricher) HasMappers() bool { return true }

func (m *mockEnricher) LocalizeByID(_ context.Context, videoID string, _ models.ContentType, _ string) (string, string, error) {
	m.mu.Lock()
	m.calls = append(m.calls, videoID)
	m.mu.Unlock()
	if m.errID[videoID] {
		return "", "", errors.New("tmdb timeout")
	}
	r, ok := m.byID[videoID]
	if !ok {
		return "", "", nil
	}
	return r[0], r[1], nil
}

func TestLocalizeItems_FiltersAndMaps(t *testing.T) {
	en := &mockEnricher{byID: map[string][2]string{
		"tt1": {"Матрица", "Сюжет"},
	}}
	got := localizeItems(context.Background(), en, "ru", []localizeRequestItem{
		{ID: "tt1", Type: "movie"},
		{ID: "tt1", Type: "movie"},      // duplicate — one pipeline call
		{ID: "tmdb42", Type: "series"},  // bare tmdb id — rejected (namespace-ambiguous)
		{ID: "tt2:1:3", Type: "series"}, // episode id — skipped
		{ID: "custom-addon-id"},         // non-imdb — skipped
	})

	if len(got) != 1 {
		t.Fatalf("expected 1 localized item, got %d: %v", len(got), got)
	}
	if got["tt1"].Title != "Матрица" || got["tt1"].Plot != "Сюжет" {
		t.Errorf("tt1 mismatch: %+v", got["tt1"])
	}
	if len(en.calls) != 1 {
		t.Errorf("expected 1 pipeline call (deduped + filtered), got %d: %v", len(en.calls), en.calls)
	}
}

func TestLocalizeItems_NoneVsError(t *testing.T) {
	en := &mockEnricher{
		byID:  map[string][2]string{"tt1": {"Матрица", ""}},
		errID: map[string]bool{"tt-flaky": true},
	}
	got := localizeItems(context.Background(), en, "ru", []localizeRequestItem{
		{ID: "tt1", Type: "movie"},
		{ID: "tt-none", Type: "movie"},  // checked, no translation → explicit empty entry
		{ID: "tt-flaky", Type: "movie"}, // pipeline error → omitted (client retries)
	})

	if _, ok := got["tt-none"]; !ok {
		t.Errorf("expected explicit empty entry for checked-no-translation id, got %v", got)
	}
	if v := got["tt-none"]; v.Title != "" || v.Plot != "" {
		t.Errorf("tt-none should be empty, got %+v", v)
	}
	if _, ok := got["tt-flaky"]; ok {
		t.Errorf("errored id must be omitted so the client retries, got %+v", got["tt-flaky"])
	}
	if got["tt1"].Title != "Матрица" {
		t.Errorf("tt1 mismatch: %+v", got["tt1"])
	}
}

func TestLocalizeItems_EnglishShortCircuits(t *testing.T) {
	en := &mockEnricher{byID: map[string][2]string{"tt1": {"X", "Y"}}}
	got := localizeItems(context.Background(), en, "en", []localizeRequestItem{{ID: "tt1", Type: "movie"}})
	if len(got) != 0 || len(en.calls) != 0 {
		t.Errorf("expected no work for en, got %v (calls %v)", got, en.calls)
	}
}

func TestLocalizeItems_CapsBatch(t *testing.T) {
	en := &mockEnricher{byID: map[string][2]string{}}
	items := make([]localizeRequestItem, 0, maxLocalizeIDs+50)
	for i := 0; i < maxLocalizeIDs+50; i++ {
		items = append(items, localizeRequestItem{ID: "tt" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: "movie"})
	}
	localizeItems(context.Background(), en, "ru", items)
	if len(en.calls) > maxLocalizeIDs {
		t.Errorf("expected at most %d pipeline calls, got %d", maxLocalizeIDs, len(en.calls))
	}
}
