package resource

import (
	"fmt"

	"github.com/webtor-io/web-ui/models"
)

// PathAction holds ready-to-use mark/unmark endpoints for a single file path
// inside a torrent. The handler pre-builds these so the file-browser template
// can render an inline watched toggle without knowing anything about
// video_id / season / episode resolution.
type PathAction struct {
	MarkURL   string
	UnmarkURL string
}

// buildPathActions maps every file path that corresponds to an enriched
// movie or episode to its user_video_status mark/unmark endpoints. Paths
// that are not movies or episodes (subtitles, samples, NFOs, directories)
// are omitted — the template skips them, no toggle is rendered.
func buildPathActions(movie *models.Movie, series *models.Series) map[string]*PathAction {
	result := map[string]*PathAction{}

	if movie != nil && movie.MovieMetadata != nil && movie.MovieMetadata.VideoID != "" && movie.Path != nil && *movie.Path != "" {
		vid := movie.MovieMetadata.VideoID
		result[*movie.Path] = &PathAction{
			MarkURL:   fmt.Sprintf("/library/movie/%s/mark", vid),
			UnmarkURL: fmt.Sprintf("/library/movie/%s/unmark", vid),
		}
	}

	if series != nil && series.SeriesMetadata != nil && series.SeriesMetadata.VideoID != "" {
		vid := series.SeriesMetadata.VideoID
		for _, ep := range series.Episodes {
			if ep.Path == nil || *ep.Path == "" || ep.Season == nil || ep.Episode == nil {
				continue
			}
			result[*ep.Path] = &PathAction{
				MarkURL:   fmt.Sprintf("/library/series/%s/episode/%d/%d/mark", vid, *ep.Season, *ep.Episode),
				UnmarkURL: fmt.Sprintf("/library/series/%s/episode/%d/%d/unmark", vid, *ep.Season, *ep.Episode),
			}
		}
	}

	return result
}
