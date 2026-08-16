package stremio

import "testing"

// TestResolutionBucket pins the shared vocabulary. The Discover client
// (streamPrefs.js resolutionOf) folds the same way — if this table changes,
// change it there too, or the note the UI renders and the filter the poller
// applies will disagree about the same release.
func TestResolutionBucket(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"The.Boys.S03E05.1080p.WEB-DL", "1080p"},
		{"The.Boys.S03E05.720p.WEB-DL", "720p"},
		{"The.Boys.S03E05.2160p.WEB-DL", "4k"},
		// Outside the profile vocabulary: folded into "other", the way the
		// JS client folds them — not passed through as their own bucket.
		{"The.Boys.S03E05.480p.WEB-DL", "other"},
		{"Doctor.Who.2005.S01E01.576p.DVDRip", "other"},
		{"The.Boys.S03E05.1440p.WEB-DL", "other"},
		// No resolution token at all.
		{"Пацаны / The Boys", "other"},
		{"", "other"},
	} {
		if got := ResolutionBucket(tt.name); got != tt.want {
			t.Errorf("ResolutionBucket(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
