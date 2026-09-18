package scripts

import (
	"strings"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
)

// subtitleHints decides what to tell video-info about the file. An
// explicit imdb id from embed settings wins (the embedding site knows
// its content) and is sent as-is. Otherwise the persisted enrichment is
// used when it resolved to IMDb (TMDB-only ids are useless to
// OpenSubtitles) — and then the content type decides the shape of the
// lookup:
//
//   - series: the enrichment row carries the SHOW's tt id, which means
//     anything without a season AND an episode would reach
//     OpenSubtitles as a movie lookup and come back with subtitles for
//     an arbitrary part of the show. Worse than nothing, so nothing is
//     sent unless the path parses to both.
//   - movie: send the id and nothing else. A movie whose filename
//     happens to carry an SxxEyy (release-group quirk, "S01E01" in a
//     bonus-feature name) must not turn into an episode lookup.
//
// Season and episode come from the persisted enrichment when it has them
// (ref — the episode row watch history also keys on), and from the file
// path via the same parser enrichment uses otherwise: the row was written
// by an earlier parse, so a file enriched before the parser learned a
// form may carry less than a fresh parse does, and vice versa.
// adult suppresses every hint we derived ourselves: the id names what the
// viewer is watching to a third party, and the same bit already hides the
// AI track and blurs the poster (owner ruling 2026-09-18). An explicit
// embed/API id is the caller's own declaration and passes through.
func subtitleHints(settingsImdbID string, md *models.VideoMetadata, ct models.ContentType, item *ra.ListItem, ref *models.VideoRef, adult bool) api.SubtitleHints {
	season, episode := 0, 0
	if ref != nil && ref.Kind == models.VideoRefKindEpisode && ref.Season > 0 && ref.Episode > 0 {
		season, episode = int(ref.Season), int(ref.Episode)
	} else if item != nil {
		if ti, err := enrich.MakeTorrentInfo(item); err == nil && ti != nil && ti.TorrentInfo != nil {
			// Both or neither: a lone episode number with no season is
			// not addressable at OpenSubtitles.
			if ti.Season > 0 && ti.Episode > 0 {
				season, episode = ti.Season, ti.Episode
			}
		}
	}

	if settingsImdbID != "" {
		return api.SubtitleHints{ImdbID: settingsImdbID, Season: season, Episode: episode}
	}
	if adult {
		return api.SubtitleHints{}
	}

	// The identity the ref carries beats the metadata row's: it was
	// resolved for exactly this file path, while md can be the pack-level
	// answer.
	if ref != nil && strings.HasPrefix(ref.VideoID, "tt") {
		switch ref.Kind {
		case models.VideoRefKindEpisode:
			if season == 0 || episode == 0 {
				return api.SubtitleHints{}
			}
			return api.SubtitleHints{ImdbID: ref.VideoID, Season: season, Episode: episode}
		case models.VideoRefKindMovie:
			return api.SubtitleHints{ImdbID: ref.VideoID}
		}
	}

	if md == nil || !strings.HasPrefix(md.VideoID, "tt") {
		return api.SubtitleHints{}
	}

	switch ct {
	case models.ContentTypeSeries:
		if season == 0 || episode == 0 {
			return api.SubtitleHints{}
		}
		return api.SubtitleHints{ImdbID: md.VideoID, Season: season, Episode: episode}
	case models.ContentTypeMovie:
		return api.SubtitleHints{ImdbID: md.VideoID}
	default:
		// Nothing enriched (ct == "") — no id to stand behind.
		return api.SubtitleHints{}
	}
}
