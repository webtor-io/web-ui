package scripts

import (
	"testing"

	"github.com/webtor-io/web-ui/models"
)

func TestResourceLeafTitleNamesTheEpisode(t *testing.T) {
	year := int16(2024)
	md := &models.VideoMetadata{Title: "The Gentlemen", Year: &year}
	ep := &models.VideoRef{Kind: models.VideoRefKindEpisode, Season: 1, Episode: 2}
	if got := resourceLeafTitle(md, ep, nil, nil); got != "The Gentlemen (2024) · S01E02" {
		t.Fatalf("series: got %q", got)
	}
	if got := resourceLeafTitle(md, nil, nil, nil); got != "The Gentlemen (2024)" {
		t.Fatalf("no ref: got %q", got)
	}
	film := &models.VideoRef{Kind: models.VideoRefKindMovie}
	if got := resourceLeafTitle(md, film, nil, nil); got != "The Gentlemen (2024)" {
		t.Fatalf("a film carries no tag: got %q", got)
	}
	special := &models.VideoRef{Kind: models.VideoRefKindEpisode, Season: 0, Episode: 3}
	if got := resourceLeafTitle(md, special, nil, nil); got != "The Gentlemen (2024) · S00E03" {
		t.Fatalf("a special is season 0: got %q", got)
	}
	unknown := &models.VideoRef{Kind: models.VideoRefKindEpisode}
	if got := resourceLeafTitle(md, unknown, nil, nil); got != "The Gentlemen (2024)" {
		t.Fatalf("an episode row without a number adds nothing: got %q", got)
	}
}
