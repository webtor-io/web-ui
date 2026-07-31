package helpers

import (
	"fmt"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/web"
)

type VideoContentHelper struct{}

func NewVideoContentHelper() *VideoContentHelper {
	return &VideoContentHelper{}
}

func (s *VideoContentHelper) GetTitle(m models.VideoContentWithMetadata) string {
	if md := m.GetMetadata(); md != nil {
		return md.Title
	}
	// GetContent returns the embedded *VideoContent, which is nil when the
	// relation was not loaded — see GetCachedPoster240, which already guards it.
	if c := m.GetContent(); c != nil {
		return c.Title
	}
	return ""
}

func (s *VideoContentHelper) HasYear(m models.VideoContentWithMetadata) bool {
	return s.GetYear(m) != 0
}

func (s *VideoContentHelper) GetYear(m models.VideoContentWithMetadata) int {
	// Both Year fields are nullable, and a title can be enriched without one.
	// The metadata branch used to dereference it unconditionally while the
	// content branch below already guarded it — that asymmetry panicked inside
	// the template, which surfaced as "failed to render main view ... hasYear:
	// invalid memory address or nil pointer dereference" on the library page.
	if md := m.GetMetadata(); md != nil && md.Year != nil {
		return int(*md.Year)
	}
	if c := m.GetContent(); c != nil && c.Year != nil {
		return int(*c.Year)
	}
	return 0
}

func (s *VideoContentHelper) HasRating(m models.VideoContentWithMetadata) bool {
	return s.GetRating(m) != 0
}

func (s *VideoContentHelper) GetRating(m models.VideoContentWithMetadata) float64 {
	if m.GetMetadata() != nil && m.GetMetadata().Rating != nil {
		r := *m.GetMetadata().Rating
		return r
	}
	return 0
}

func (s *VideoContentHelper) HasPoster(m models.VideoContentWithMetadata) bool {
	return s.GetOriginalPoster(m) != ""
}

func (s *VideoContentHelper) GetOriginalPoster(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().PosterURL
	}
	return ""
}

func (s *VideoContentHelper) HasVideoID(m models.VideoContentWithMetadata) bool {
	return m.GetMetadata() != nil && m.GetMetadata().VideoID != ""
}

func (s *VideoContentHelper) GetVideoID(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().VideoID
	}
	return ""
}

func (s *VideoContentHelper) GetVideoType(m models.VideoContentWithMetadata) string {
	return string(m.GetContentType())
}

// GetCachedPoster240 returns the resource-keyed poster URL at 240px.
// Delegates to web.Helper.PosterURL so the /raw/ switch stays in one
// place across all consumers (library, continue-watching, resource
// page). Per-helper indirection just lets the template call this
// pipeline-style without having to fish ResourceID + IsAdult out
// of the model in the template itself.
func (s *VideoContentHelper) GetCachedPoster240(m models.VideoContentWithMetadata, ctx *web.Context) string {
	if m.GetContent() == nil || m.GetContent().ResourceID == "" {
		return ""
	}
	isAdult := false
	if a, ok := m.(interface{ IsAdult() bool }); ok {
		isAdult = a.IsAdult()
	}
	return web.PosterURL(m.GetContent().ResourceID, 240, isAdult, ctx)
}

func (s *VideoContentHelper) HasEpisodeStill(ep *models.Episode) bool {
	return ep.EpisodeMetadata != nil && ep.EpisodeMetadata.StillURL != nil && *ep.EpisodeMetadata.StillURL != ""
}

func (s *VideoContentHelper) GetCachedEpisodeStill(ep *models.Episode, width int) string {
	if ep.EpisodeMetadata == nil || ep.Season == nil || ep.Episode == nil {
		return ""
	}
	return fmt.Sprintf("/lib/episode/still/%v/%v/%v/%v.jpg", ep.EpisodeMetadata.VideoID, *ep.Season, *ep.Episode, width)
}

func (s *VideoContentHelper) GetEpisodeTitle(ep *models.Episode) string {
	if ep.EpisodeMetadata != nil && ep.EpisodeMetadata.Title != nil && *ep.EpisodeMetadata.Title != "" {
		return *ep.EpisodeMetadata.Title
	}
	if ep.Title != nil {
		return *ep.Title
	}
	if ep.Episode != nil {
		return fmt.Sprintf("Episode %d", *ep.Episode)
	}
	return ""
}

func (s *VideoContentHelper) GetEpisodePlot(ep *models.Episode) string {
	if ep.EpisodeMetadata != nil && ep.EpisodeMetadata.Plot != nil {
		return *ep.EpisodeMetadata.Plot
	}
	return ""
}
