package release_subscription

import (
	"errors"
	"testing"

	"github.com/webtor-io/web-ui/models"
)

// TestNormalize covers the guard that keeps unpollable rows out of the
// table. Everything downstream — the Torznab query, the addon request —
// speaks bare IMDB title ids, so anything else has to be refused here
// rather than discovered by a poller that can never match it.
func TestNormalize(t *testing.T) {
	for _, tt := range []struct {
		name       string
		req        Request
		wantKind   string
		wantSeason int16
		wantNil    bool
		wantErr    bool
	}{
		{
			name:     "movie",
			req:      Request{Kind: "movie", VideoID: "tt0111161"},
			wantKind: models.ReleaseSubscriptionKindMovie,
			wantNil:  true,
		},
		{
			name:       "season",
			req:        Request{Kind: "season", VideoID: "tt1190634", Season: 3},
			wantKind:   models.ReleaseSubscriptionKindSeason,
			wantSeason: 3,
		},
		{
			name:    "season without a number",
			req:     Request{Kind: "season", VideoID: "tt1190634"},
			wantErr: true,
		},
		{
			name:    "negative season",
			req:     Request{Kind: "season", VideoID: "tt1190634", Season: -1},
			wantErr: true,
		},
		{
			// Stremio addresses an episode as tt:S:E. Storing that as the
			// content key would make the subscription follow one episode
			// that has already aired.
			name:    "episode id",
			req:     Request{Kind: "season", VideoID: "tt1190634:3:4", Season: 3},
			wantErr: true,
		},
		{
			// Library items use internal ids no external source can resolve.
			name:    "library id",
			req:     Request{Kind: "movie", VideoID: "wt-8c1f0d24"},
			wantErr: true,
		},
		{
			name:    "tmdb id",
			req:     Request{Kind: "movie", VideoID: "tmdb:603"},
			wantErr: true,
		},
		{
			name:    "unknown kind",
			req:     Request{Kind: "series", VideoID: "tt1190634"},
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kind, season, err := normalize(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got kind=%q season=%v", kind, season)
				}
				if !errors.Is(err, ErrBadRequest) {
					t.Errorf("want ErrBadRequest, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tt.wantKind {
				t.Errorf("kind: got %q, want %q", kind, tt.wantKind)
			}
			if tt.wantNil {
				if season != nil {
					t.Errorf("season: got %v, want nil", *season)
				}
				return
			}
			if season == nil {
				t.Fatalf("season: got nil, want %d", tt.wantSeason)
			}
			if *season != tt.wantSeason {
				t.Errorf("season: got %d, want %d", *season, tt.wantSeason)
			}
		})
	}
}

// TestNormalizeSource keeps the analytics column readable: a source we know
// is stored as-is, anything else collapses to "other" instead of failing the
// request, so an older client posting a renamed surface still works.
func TestNormalizeSource(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"discover_season", "discover_season"},
		{"resource_banner", "resource_banner"},
		{"empty_streams", "empty_streams"},
		{"empty_filters", "empty_filters"},
		{"profile", "profile"},
		{"", "other"},
		{"whatever", "other"},
	} {
		if got := normalizeSource(tt.in); got != tt.want {
			t.Errorf("normalizeSource(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLimit(t *testing.T) {
	if got := Limit(true); got != FreeTierLimit {
		t.Errorf("free tier limit: got %d, want %d", got, FreeTierLimit)
	}
	if got := Limit(false); got != -1 {
		t.Errorf("paid tier limit: got %d, want -1 (unlimited)", got)
	}
}

// TestCheckEligibleWithoutEnricher pins the conservative branch: with no
// mappers wired the airing check cannot run, and a season subscription is
// refused rather than accepted blind. A movie stays acceptable — nothing
// local could tell us whether a release is coming anyway.
func TestCheckEligibleWithoutEnricher(t *testing.T) {
	s := New(nil, nil, nil, "", "")
	if err := s.checkEligible(t.Context(), models.ReleaseSubscriptionKindSeason, "tt1190634"); !errors.Is(err, ErrNotEligible) {
		t.Errorf("season without enricher: got %v, want ErrNotEligible", err)
	}
	if err := s.checkEligible(t.Context(), models.ReleaseSubscriptionKindMovie, "tt0111161"); err != nil {
		t.Errorf("movie without enricher: got %v, want nil", err)
	}
}
