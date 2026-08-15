package link_resolver

import (
	"context"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// fakeLister serves a canned listing one page at a time, so the walk itself —
// paging, the video filter, the preferred-over-fallback rule — is exercised
// without an HTTP client. pageSize mimics a server that returns fewer items
// than asked for.
type fakeLister struct {
	items    []ra.ListItem
	pageSize int
	calls    int
	err      error
}

func (f *fakeLister) ListResourceContentCached(_ context.Context, _ *api.Claims, _ string, args *api.ListResourceContentArgs) (*ra.ListResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	size := f.pageSize
	if size <= 0 || size > int(args.Limit) {
		size = int(args.Limit)
	}
	start := int(args.Offset)
	if start > len(f.items) {
		start = len(f.items)
	}
	end := start + size
	if end > len(f.items) {
		end = len(f.items)
	}
	return &ra.ListResponse{Count: len(f.items), Items: f.items[start:end]}, nil
}

func idxOrFail(t *testing.T, items []ra.ListItem, preferred func(ra.ListItem) bool) (int, error) {
	t.Helper()
	return pickFileIdx(context.Background(), &fakeLister{items: items}, nil, "hash", preferred)
}

// TestPickFileIdxPrimary covers the choice made for a stream whose source
// named a torrent but not a file — a Torznab search result. Index 0 is the
// torrent's first file, which in a real release is as likely to be a sample
// or an .nfo as the feature itself.
func TestPickFileIdxPrimary(t *testing.T) {
	for _, tt := range []struct {
		name    string
		items   []ra.ListItem
		wantIdx int
		wantErr bool
	}{
		{
			name: "the sample listed first does not win",
			items: []ra.ListItem{
				{Index: 0, Size: 40 << 20, MediaFormat: ra.Video},  // sample
				{Index: 1, Size: 8 << 30, MediaFormat: ra.Video},   // feature
				{Index: 2, Size: 2 << 10, MediaFormat: ra.Unknown}, // .nfo
			},
			wantIdx: 1,
		},
		{
			name: "a larger non-video file is ignored",
			items: []ra.ListItem{
				{Index: 0, Size: 30 << 30, MediaFormat: ra.Unknown},
				{Index: 1, Size: 1 << 30, MediaFormat: ra.Video},
			},
			wantIdx: 1,
		},
		{
			name:    "no video at all",
			items:   []ra.ListItem{{Index: 0, Size: 1 << 20, MediaFormat: ra.Unknown}},
			wantErr: true,
		},
		{name: "empty listing", items: nil, wantErr: true},
		{
			// The index is the torrent's natural order, not the position in
			// the page — a listing sorted folders-first would otherwise
			// resolve the wrong file.
			name: "index comes from the item, not its position",
			items: []ra.ListItem{
				{Index: 7, Size: 8 << 30, MediaFormat: ra.Video},
				{Index: 3, Size: 1 << 30, MediaFormat: ra.Video},
			},
			wantIdx: 7,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			idx, err := idxOrFail(t, tt.items, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && idx != tt.wantIdx {
				t.Errorf("index = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}

// TestPickFileIdxEpisode covers the rule that makes an indexer usable for
// series: inside a season pack the episode the user asked for wins over the
// largest file, and a pack that names no episode still plays something.
func TestPickFileIdxEpisode(t *testing.T) {
	pack := []ra.ListItem{
		{Index: 0, Size: 4 << 30, PathStr: "Breaking.Bad.S01E01.mkv", MediaFormat: ra.Video},
		{Index: 1, Size: 3 << 30, PathStr: "Breaking.Bad.S01E05.mkv", MediaFormat: ra.Video},
		{Index: 2, Size: 8 << 30, PathStr: "extras/behind.the.scenes.mkv", MediaFormat: ra.Video},
	}
	// The requested episode beats both the first file and the biggest one.
	if idx, err := idxOrFail(t, pack, newEpisodeMatcher(1, 5)); err != nil || idx != 1 {
		t.Errorf("episode pick = %d (%v), want 1", idx, err)
	}
	// No file names the episode: the largest video is the only sane answer.
	if idx, err := idxOrFail(t, pack, newEpisodeMatcher(1, 9)); err != nil || idx != 2 {
		t.Errorf("fallback pick = %d (%v), want 2", idx, err)
	}
	// Two files name it (a pack plus a re-encode) — the bigger one wins.
	dup := append([]ra.ListItem{}, pack...)
	dup = append(dup, ra.ListItem{Index: 3, Size: 6 << 30, PathStr: "Breaking Bad 1x05 REMUX.mkv", MediaFormat: ra.Video})
	if idx, err := idxOrFail(t, dup, newEpisodeMatcher(1, 5)); err != nil || idx != 3 {
		t.Errorf("largest matching pick = %d (%v), want 3", idx, err)
	}
	// A subtitle file naming the episode must not be played instead of it.
	subs := []ra.ListItem{
		{Index: 0, Size: 30 << 10, PathStr: "Breaking.Bad.S01E05.srt", MediaFormat: ra.Subtitle},
		{Index: 1, Size: 3 << 30, PathStr: "Breaking.Bad.S01E05.mkv", MediaFormat: ra.Video},
	}
	if idx, err := idxOrFail(t, subs, newEpisodeMatcher(1, 5)); err != nil || idx != 1 {
		t.Errorf("subtitle pick = %d (%v), want 1", idx, err)
	}
}

// TestPickFileIdxPagesThroughTheWholeTorrent guards the walk itself: a match
// that lives past the first page has to be found, and the loop has to stop.
func TestPickFileIdxPagesThroughTheWholeTorrent(t *testing.T) {
	var items []ra.ListItem
	for i := 0; i < 250; i++ {
		items = append(items, ra.ListItem{Index: i, Size: int64(i+1) << 20, PathStr: "filler.mkv", MediaFormat: ra.Video})
	}
	items[200] = ra.ListItem{Index: 200, Size: 1 << 20, PathStr: "Show.S02E07.mkv", MediaFormat: ra.Video}

	l := &fakeLister{items: items, pageSize: 100}
	idx, err := pickFileIdx(context.Background(), l, nil, "hash", newEpisodeMatcher(2, 7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 200 {
		t.Errorf("index = %d, want 200 (the match on the third page)", idx)
	}
	if l.calls != 3 {
		t.Errorf("listing calls = %d, want 3", l.calls)
	}
}

// TestEpisodeMatcher covers the naming shapes a season pack actually uses.
// Getting this wrong is not a cosmetic miss: the fallback is "largest video
// in the torrent", which for a pack means episode one regardless of what the
// user picked.
func TestEpisodeMatcher(t *testing.T) {
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
		// RU packs name files by episode alone, with the season only in the
		// directory above. Without this the whole shape fell through to
		// "largest video" — i.e. a random episode.
		{"Во все тяжкие/05 серия.mkv", 1, 5, true},
		{"Во все тяжкие/5-я серия.mkv", 1, 5, true},
		{"Во все тяжкие/1 сезон 5 серия.mkv", 1, 5, true},
		{"Во все тяжкие/06 серия.mkv", 1, 5, false},
		{"Во все тяжкие/01-10 серии.mkv", 1, 5, false},
		{"Эпизод 5.mkv", 1, 5, true},
		// A pack that names no episode at all falls through to the
		// largest-file fallback rather than matching by accident.
		{"Breaking Bad Season 1 Complete/disc1.mkv", 1, 5, false},
		{"sample.mkv", 1, 5, false},
	} {
		match := newEpisodeMatcher(tt.season, tt.episode)
		if match == nil {
			t.Fatalf("no matcher for s%de%d", tt.season, tt.episode)
		}
		if got := match(ra.ListItem{PathStr: tt.path}); got != tt.want {
			t.Errorf("match(%q) for S%02dE%02d = %v, want %v", tt.path, tt.season, tt.episode, got, tt.want)
		}
	}
	if newEpisodeMatcher(0, 5) != nil {
		t.Error("a matcher with no season must be nil, so the caller falls back to the largest video")
	}
}
