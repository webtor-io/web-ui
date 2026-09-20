package scripts

import (
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/next_item"
)

func i16(v int16) *int16   { return &v }
func str(v string) *string { return &v }

func TestEpisodesForPickDropsRowsItCannotOrder(t *testing.T) {
	rows := []*models.Episode{
		{Season: i16(1), Episode: i16(2), Path: str("e02.mkv"), Title: str("Two")},
		{Season: i16(1), Episode: nil, Path: str("unknown.mkv")},
		{Season: nil, Episode: i16(3), Path: str("x.mkv")},
		nil,
		{Season: i16(1), Episode: i16(3)}, // no file in this torrent: kept, with an empty path
	}
	got := episodesForPick(rows)
	if len(got) != 2 {
		t.Fatalf("want 2 orderable rows, got %d: %+v", len(got), got)
	}
	if got[0] != (next_item.Episode{Season: 1, Episode: 2, Path: "e02.mkv", Title: "Two"}) {
		t.Fatalf("unexpected first row %+v", got[0])
	}
	if got[1].Path != "" {
		t.Fatalf("a row without a file keeps an empty path, got %q", got[1].Path)
	}
}

func TestNextItemLabel(t *testing.T) {
	if got := nextItemLabel(&next_item.Pick{Kind: next_item.KindEpisode, Season: 1, Episode: 3, Title: "Pilot"}); got != "S01E03 · Pilot" {
		t.Fatalf("episode with a title: %q", got)
	}
	if got := nextItemLabel(&next_item.Pick{Kind: next_item.KindEpisode, Season: 2, Episode: 10}); got != "S02E10" {
		t.Fatalf("episode without a title: %q", got)
	}
	if got := nextItemLabel(&next_item.Pick{Kind: next_item.KindTrack, Title: "02 - Song"}); got != "02 - Song" {
		t.Fatalf("track: %q", got)
	}
}
