package resource

import (
	"context"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

// ReleaseSubscribeBanner is the fake-door experiment surface for release-level
// subscription. Populated on resource page render when the resource maps to a
// currently-airing series; rendered as a non-blocking banner with Yes/No/Close
// actions, all tracked via Umami only (no DB writes). See
// docs/release_sub_fake_door.md for the experiment plan and decision gates.
type ReleaseSubscribeBanner struct {
	Visible         bool
	SeriesTitle     string
	SeriesVideoID   string
	Season          int
	ReleaseGroupRaw string
}

// prepareReleaseSubscribeBanner — experiment concluded 2026-05-24, banner
// disabled. Decision: aggregate CTR 2.2% < 5% gate (broad hypothesis
// rejected), but paid CTR 11-25% validated future-engagement signaling as a
// paid-tier feature class. See docs/release_sub_fake_door.md "Результаты"
// for full breakdown. Type + template + JS + locale keys kept as scaffold
// for the real paid feature; helpers (dominantSeason / extractReleaseGroup)
// removed — re-add from git when reviving.
func prepareReleaseSubscribeBanner(ctx context.Context, en *enrich.Enricher, res *ra.ResourceResponse, series *models.Series) *ReleaseSubscribeBanner {
	return nil
}
