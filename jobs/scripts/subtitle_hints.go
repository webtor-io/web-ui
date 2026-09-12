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
// its content); otherwise the persisted enrichment is used when it
// resolved to IMDb (TMDB-only ids are useless to OpenSubtitles).
// Season and episode come from the file path via the same parser
// enrichment uses, so series files hit the episode endpoint.
func subtitleHints(settingsImdbID string, md *models.VideoMetadata, item *ra.ListItem) api.SubtitleHints {
	h := api.SubtitleHints{ImdbID: settingsImdbID}
	if h.ImdbID == "" && md != nil && strings.HasPrefix(md.VideoID, "tt") {
		h.ImdbID = md.VideoID
	}
	if h.ImdbID == "" || item == nil {
		return h
	}
	if ti, err := enrich.MakeTorrentInfo(item); err == nil && ti != nil && ti.TorrentInfo != nil {
		h.Season = ti.Season
		h.Episode = ti.Episode
	}
	return h
}
