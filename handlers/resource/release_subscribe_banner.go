package resource

import (
	"context"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
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

// prepareReleaseSubscribeBanner returns banner data when the resource is a
// series in airing state. Airing detection goes through the Enricher's
// mapper-agnostic AiringChecker capability (TMDB owns the actual `status` /
// `in_production` interpretation; this layer doesn't care which mapper
// answered). No external calls — all signals come from local enrichment
// cache plus a torrent-name parse.
func prepareReleaseSubscribeBanner(ctx context.Context, en *enrich.Enricher, res *ra.ResourceResponse, series *models.Series) *ReleaseSubscribeBanner {
	if en == nil || series == nil || series.SeriesMetadata == nil || series.SeriesMetadata.VideoID == "" {
		return nil
	}
	if !en.IsAiringSeries(ctx, series.SeriesMetadata.VideoID) {
		return nil
	}
	return &ReleaseSubscribeBanner{
		Visible:         true,
		SeriesTitle:     series.SeriesMetadata.Title,
		SeriesVideoID:   series.SeriesMetadata.VideoID,
		Season:          dominantSeason(series),
		ReleaseGroupRaw: extractReleaseGroup(res),
	}
}

// dominantSeason returns the season number that has the largest number of
// episodes in this torrent. For per-episode torrents this is just that
// episode's season; for season packs it's the pack's season. Returns 0 when
// no episode has a season set.
func dominantSeason(series *models.Series) int {
	counts := map[int]int{}
	for _, ep := range series.Episodes {
		if ep.Season == nil {
			continue
		}
		counts[int(*ep.Season)]++
	}
	best, bestCount := 0, 0
	for s, c := range counts {
		if c > bestCount {
			best, bestCount = s, c
		}
	}
	return best
}

// extractReleaseGroup parses the torrent's top-level name through
// parse_torrent_name and returns the `Group` field as-is. Empty when the
// parser couldn't find a -GROUP suffix; that's a tracked data point in the
// experiment, not an error.
func extractReleaseGroup(res *ra.ResourceResponse) string {
	if res == nil || res.Name == "" {
		return ""
	}
	info := &ptn.TorrentInfo{}
	if _, err := ptn.Parse(info, res.Name); err != nil {
		return ""
	}
	return info.Group
}
