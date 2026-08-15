package resource

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

// ReleaseSubscribeBanner is the subscribe surface on a torrent page: when
// the torrent is an episode of a season that is still airing, the page
// offers to watch for new releases of that season.
//
// It is the same slot the 2026 fake-door used (see
// docs/release_sub_fake_door.md); what was a survey is now the feature.
type ReleaseSubscribeBanner struct {
	SeriesTitle   string
	SeriesVideoID string
	Season        int
	// Subscribed and SubscriptionID turn the banner into its other state:
	// the viewer already follows this season, and the button unsubscribes.
	Subscribed     bool
	SubscriptionID string
	// Anonymous viewers see the offer but cannot act on it — the button
	// becomes a login link rather than disappearing, because "we can tell
	// you when this updates" is a reason to have an account.
	Anonymous bool
}

// bannerAiring is the "is this series still producing episodes" capability,
// as an interface so the banner's rule can be tested without a TMDB cache.
type bannerAiring interface {
	IsAiringSeries(ctx context.Context, videoID string) bool
}

// bannerSubs answers "does this viewer already follow this season".
type bannerSubs interface {
	Find(ctx context.Context, userID uuid.UUID, videoID string, season int) (*models.ReleaseSubscription, error)
}

// pgBannerSubs is the production lookup.
type pgBannerSubs struct{ db *pg.DB }

func (s pgBannerSubs) Find(ctx context.Context, userID uuid.UUID, videoID string, season int) (*models.ReleaseSubscription, error) {
	if s.db == nil {
		return nil, nil
	}
	sn := int16(season)
	return models.FindUserReleaseSubscription(ctx, s.db, userID, models.ReleaseSubscriptionKindSeason, videoID, &sn)
}

// prepareReleaseSubscribeBanner decides whether the page shows the offer,
// and in which of its two states.
//
// Three things have to hold: the torrent maps to a series we have metadata
// for, the torrent is about one identifiable season, and that series is
// still in production. The last one goes through the Enricher's
// mapper-agnostic AiringChecker capability — this layer never reads TMDB's
// `status` itself.
//
// Everything here comes from the local enrichment cache, so it costs the
// page no external call.
func prepareReleaseSubscribeBanner(ctx context.Context, airing bannerAiring, subs bannerSubs, user *auth.User, series *models.Series) *ReleaseSubscribeBanner {
	if airing == nil || series == nil || series.SeriesMetadata == nil || series.SeriesMetadata.VideoID == "" {
		return nil
	}
	season := dominantSeason(series)
	if season == 0 {
		// A torrent whose episodes carry no season number cannot address a
		// subscription: the poller would not know what to ask for.
		return nil
	}
	if !airing.IsAiringSeries(ctx, series.SeriesMetadata.VideoID) {
		return nil
	}

	b := &ReleaseSubscribeBanner{
		SeriesTitle:   series.SeriesMetadata.Title,
		SeriesVideoID: series.SeriesMetadata.VideoID,
		Season:        season,
	}

	if user == nil || !user.HasAuth() {
		b.Anonymous = true
		return b
	}
	if subs == nil {
		return b
	}
	// A failed lookup renders the subscribe state. Offering to subscribe to
	// something already subscribed is a no-op on the server (the write path
	// answers from the existing row), while the reverse would show an
	// unsubscribe button that removes nothing.
	sub, err := subs.Find(ctx, user.ID, b.SeriesVideoID, season)
	if err == nil && sub != nil {
		b.Subscribed = true
		b.SubscriptionID = sub.ID.String()
	}
	return b
}

// dominantSeason returns the season number that has the largest number of
// episodes in this torrent. For per-episode torrents this is just that
// episode's season; for season packs it's the pack's season. Returns 0 when
// the torrent names no subscribable season.
//
// Specials are excluded rather than counted: season 0 doubles as the "none
// found" answer, so a torrent carrying five extras and three first-season
// episodes would otherwise resolve to 0 and suppress the banner for a
// season that is perfectly addressable.
//
// Ties are broken by the lower season number, so a torrent holding two half
// seasons resolves the same way on every render rather than following map
// iteration order.
func dominantSeason(series *models.Series) int {
	counts := map[int]int{}
	for _, ep := range series.Episodes {
		if ep.Season == nil || *ep.Season <= 0 {
			continue
		}
		counts[int(*ep.Season)]++
	}
	best, bestCount := 0, 0
	for s, c := range counts {
		if c > bestCount || (c == bestCount && best != 0 && s < best) {
			best, bestCount = s, c
		}
	}
	return best
}
