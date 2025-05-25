package helpers

import (
	"fmt"
	"github.com/webtor-io/web-ui/models"
)

type VideoContentHelper struct{}

func NewVideoContentHelper() *VideoContentHelper {
	return &VideoContentHelper{}
}

func (s *VideoContentHelper) GetTitle(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().Title
	}
	return m.GetContent().Title
}

func (s *VideoContentHelper) HasYear(m models.VideoContentWithMetadata) bool {
	return s.GetYear(m) != 0
}

func (s *VideoContentHelper) GetYear(m models.VideoContentWithMetadata) int {
	if m.GetMetadata() != nil {
		y := *m.GetMetadata().Year
		return int(y)
	}
	if m.GetContent().Year == nil {
		return 0
	}
	y := *m.GetContent().Year
	return int(y)
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

func (s *VideoContentHelper) GetCachedPoster240(m models.VideoContentWithMetadata) string {
	return fmt.Sprintf("/lib/%v/poster/%v/240.jpg", m.GetContentType(), m.GetMetadata().VideoID)
}
