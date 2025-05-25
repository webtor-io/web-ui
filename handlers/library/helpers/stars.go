package helpers

import (
	"fmt"
)

type Star struct {
	Value    float64
	Title    string
	Selected bool
	HalfStep bool
}

func (s *StarsHelper) MakeStars(r float64) (stars []Star) {
	step := 0.5
	maxStar := 5.0
	maxRating := 10.0
	rating := r / maxRating * maxStar
	for i := float64(0); i <= maxStar; i = i + step {
		stars = append(stars, Star{
			Value:    i,
			Title:    fmt.Sprintf("%.1f", i),
			Selected: rating >= i && rating < i+step,
			HalfStep: int((i-float64(int(i)))*2) == 1,
		})
	}
	return stars
}

type StarsHelper struct {
}

func NewStarsHelper() *StarsHelper {
	return &StarsHelper{}
}
