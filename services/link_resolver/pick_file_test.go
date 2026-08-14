package link_resolver

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// TestPickLargestVideo covers the choice made for a stream whose source
// named a torrent but not a file — a Torznab search result. Index 0 is the
// torrent's first file, which in a real release is as likely to be a sample
// or an .nfo as the feature itself.
func TestPickLargestVideo(t *testing.T) {
	for _, tt := range []struct {
		name    string
		items   []ra.ListItem
		wantIdx int
		wantOK  bool
	}{
		{
			name: "the sample listed first does not win",
			items: []ra.ListItem{
				{Index: 0, Size: 40 << 20, MediaFormat: ra.Video},  // sample
				{Index: 1, Size: 8 << 30, MediaFormat: ra.Video},   // feature
				{Index: 2, Size: 2 << 10, MediaFormat: ra.Unknown}, // .nfo
			},
			wantIdx: 1,
			wantOK:  true,
		},
		{
			name: "a larger non-video file is ignored",
			items: []ra.ListItem{
				{Index: 0, Size: 30 << 30, MediaFormat: ra.Unknown},
				{Index: 1, Size: 1 << 30, MediaFormat: ra.Video},
			},
			wantIdx: 1,
			wantOK:  true,
		},
		{
			name:   "no video at all",
			items:  []ra.ListItem{{Index: 0, Size: 1 << 20, MediaFormat: ra.Unknown}},
			wantOK: false,
		},
		{name: "empty listing", items: nil, wantOK: false},
		{
			// The index is the torrent's natural order, not the position in
			// this page — a listing sorted folders-first would otherwise
			// resolve the wrong file.
			name: "index comes from the item, not its position",
			items: []ra.ListItem{
				{Index: 7, Size: 8 << 30, MediaFormat: ra.Video},
				{Index: 3, Size: 1 << 30, MediaFormat: ra.Video},
			},
			wantIdx: 7,
			wantOK:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			idx, _, ok := pickLargestVideo(tt.items)
			if ok != tt.wantOK {
				t.Fatalf("found = %v, want %v", ok, tt.wantOK)
			}
			if ok && idx != tt.wantIdx {
				t.Errorf("index = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}

// TestMatchesEpisode covers the naming shapes a season pack actually uses.
// Getting this wrong is not a cosmetic miss: the fallback is "largest video
// in the torrent", which for a pack means episode one regardless of what the
// user picked.
func TestMatchesEpisode(t *testing.T) {
	for _, tt := range []struct {
		path            string
		season, episode int
		want            bool
	}{
		{"Breaking.Bad.S01E05.1080p.mkv", 1, 5, true},
		{"Breaking Bad - S01E05 - Gray Matter.mkv", 1, 5, true},
		{"breaking.bad.s1e5.mkv", 1, 5, true},
		{"Season 1/Breaking Bad 1x05.mkv", 1, 5, true},
		{"Breaking.Bad.S01E05.1080p.mkv", 1, 6, false},
		// The neighbouring episode must not match, which is what the
		// non-digit anchor is for.
		{"Breaking.Bad.S01E050.mkv", 1, 5, false},
		{"Breaking.Bad.S02E05.mkv", 1, 5, false},
		// A pack that names no episode at all falls through to the
		// largest-file fallback rather than matching by accident.
		{"Breaking Bad Season 1 Complete/disc1.mkv", 1, 5, false},
		{"sample.mkv", 1, 5, false},
		{"Breaking.Bad.S01E05.mkv", 0, 5, false},
	} {
		if got := matchesEpisode(tt.path, tt.season, tt.episode); got != tt.want {
			t.Errorf("matchesEpisode(%q, %d, %d) = %v, want %v", tt.path, tt.season, tt.episode, got, tt.want)
		}
	}
}
