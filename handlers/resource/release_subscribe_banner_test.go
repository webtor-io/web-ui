package resource

import (
	"context"
	"errors"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

type fakeAiring struct {
	airing      bool
	asked       string
	askedSeason int
}

func (f *fakeAiring) IsAiringSeason(_ context.Context, videoID string, season int) bool {
	f.asked = videoID
	f.askedSeason = season
	return f.airing
}

type fakeSubs struct {
	sub *models.ReleaseSubscription
	err error
}

func (f *fakeSubs) Find(context.Context, uuid.UUID, string, int) (*models.ReleaseSubscription, error) {
	return f.sub, f.err
}

func season(n int16) *int16 { return &n }

// withNullSeasons adds episodes the parser could not place into a season.
func withNullSeasons(s *models.Series) *models.Series {
	s.Episodes = append(s.Episodes, &models.Episode{}, &models.Episode{})
	return s
}

func seriesWith(videoID string, seasons ...int16) *models.Series {
	s := &models.Series{
		SeriesMetadata: &models.SeriesMetadata{
			VideoMetadata: &models.VideoMetadata{VideoID: videoID, Title: "The Boys"},
		},
	}
	for _, sn := range seasons {
		s.Episodes = append(s.Episodes, &models.Episode{Season: season(sn)})
	}
	return s
}

var signedIn = &auth.User{ID: uuid.NewV4(), Email: "viewer@example.com"}

func TestBannerOfferedForAnAiringSeason(t *testing.T) {
	airing := &fakeAiring{airing: true}
	b := prepareReleaseSubscribeBanner(context.Background(), airing, &fakeSubs{}, signedIn, seriesWith("tt1190634", 3, 3, 3))

	if b == nil {
		t.Fatal("no banner")
	}
	if b.Season != 3 || b.SeriesVideoID != "tt1190634" || b.SeriesTitle != "The Boys" {
		t.Errorf("banner: %+v", b)
	}
	if b.Subscribed || b.Anonymous {
		t.Errorf("state: subscribed=%v anonymous=%v, want the plain offer", b.Subscribed, b.Anonymous)
	}
	if airing.asked != "tt1190634" || airing.askedSeason != 3 {
		t.Errorf("airing check asked about %q season %d, want tt1190634 season 3 — the check is per season", airing.asked, airing.askedSeason)
	}
}

// A series that has finished producing episodes has no next release to wait
// for, so the page says nothing at all rather than offering a subscription
// the write path would refuse.
func TestNoBannerWhenTheSeriesHasEnded(t *testing.T) {
	b := prepareReleaseSubscribeBanner(context.Background(), &fakeAiring{airing: false}, &fakeSubs{}, signedIn, seriesWith("tt1190634", 3))
	if b != nil {
		t.Errorf("banner rendered for a finished series: %+v", b)
	}
}

func TestNoBannerWithoutIdentifiableContent(t *testing.T) {
	airing := &fakeAiring{airing: true}
	for _, tt := range []struct {
		name   string
		series *models.Series
	}{
		{"no series at all", nil},
		{"series without metadata", &models.Series{}},
		{"metadata without a video id", &models.Series{SeriesMetadata: &models.SeriesMetadata{VideoMetadata: &models.VideoMetadata{}}}},
		{"no episode carries a season", seriesWith("tt1190634")},
		{
			// A torrent whose files the parser could not place: the poller
			// would have no season to query.
			name:   "episodes with null seasons",
			series: withNullSeasons(seriesWith("tt1190634")),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if b := prepareReleaseSubscribeBanner(context.Background(), airing, &fakeSubs{}, signedIn, tt.series); b != nil {
				t.Errorf("banner rendered: %+v", b)
			}
		})
	}
}

// An anonymous visitor still sees the offer — it is a reason to have an
// account — but the banner marks itself so the template renders a login
// link instead of a form that would 401.
func TestAnonymousGetsTheOfferWithoutALookup(t *testing.T) {
	subs := &fakeSubs{err: errors.New("must not be called")}
	b := prepareReleaseSubscribeBanner(context.Background(), &fakeAiring{airing: true}, subs, &auth.User{}, seriesWith("tt1190634", 2))
	if b == nil {
		t.Fatal("no banner")
	}
	if !b.Anonymous {
		t.Error("banner not marked anonymous")
	}
	if b.Subscribed {
		t.Error("an anonymous visitor cannot be subscribed")
	}
}

func TestBannerFlipsToUnsubscribeWhenAlreadyFollowing(t *testing.T) {
	id := uuid.NewV4()
	subs := &fakeSubs{sub: &models.ReleaseSubscription{ID: id}}
	b := prepareReleaseSubscribeBanner(context.Background(), &fakeAiring{airing: true}, subs, signedIn, seriesWith("tt1190634", 4))

	if b == nil {
		t.Fatal("no banner")
	}
	if !b.Subscribed || b.SubscriptionID != id.String() {
		t.Errorf("state: subscribed=%v id=%q, want the unsubscribe state", b.Subscribed, b.SubscriptionID)
	}
}

// A lookup failure renders the subscribe state: subscribing to something
// already subscribed is a no-op on the server, while the reverse would draw
// an unsubscribe button that removes nothing.
func TestLookupFailureFallsBackToTheOffer(t *testing.T) {
	subs := &fakeSubs{err: errors.New("db down")}
	b := prepareReleaseSubscribeBanner(context.Background(), &fakeAiring{airing: true}, subs, signedIn, seriesWith("tt1190634", 1))
	if b == nil {
		t.Fatal("no banner")
	}
	if b.Subscribed {
		t.Error("a failed lookup must not claim the viewer is subscribed")
	}
}

func TestDominantSeason(t *testing.T) {
	for _, tt := range []struct {
		name    string
		seasons []int16
		want    int
	}{
		{"single episode", []int16{5}, 5},
		{"season pack", []int16{2, 2, 2, 2}, 2},
		{"pack plus a stray episode from another season", []int16{3, 3, 3, 4}, 3},
		{"no seasons at all", nil, 0},
		// Ties would otherwise follow map iteration order and pick a
		// different season on every render of the same page.
		{"tie resolves to the lower season", []int16{1, 1, 2, 2}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := dominantSeason(seriesWith("tt1", tt.seasons...)); got != tt.want {
				t.Errorf("dominantSeason: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDominantSeasonIgnoresEpisodesWithoutASeason(t *testing.T) {
	s := seriesWith("tt1", 7)
	s.Episodes = append(s.Episodes, &models.Episode{}, &models.Episode{})
	if got := dominantSeason(s); got != 7 {
		t.Errorf("dominantSeason: got %d, want 7", got)
	}
}

// Season 0 is both "specials" and the sentinel for "no season found", so a
// torrent whose extras outnumber its episodes would suppress the banner for
// a season the poller could perfectly well follow.
func TestDominantSeasonIgnoresSpecials(t *testing.T) {
	s := seriesWith("tt1190634", 0, 0, 0, 0, 0, 1, 1, 1)
	if got := dominantSeason(s); got != 1 {
		t.Errorf("dominantSeason: got %d, want 1 — specials must not win", got)
	}

	// A torrent of nothing but extras still names no subscribable season.
	if got := dominantSeason(seriesWith("tt1190634", 0, 0)); got != 0 {
		t.Errorf("dominantSeason: got %d, want 0 for a specials-only torrent", got)
	}
}
