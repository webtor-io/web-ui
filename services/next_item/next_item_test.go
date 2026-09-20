package next_item

import "testing"

func eps(list ...Episode) []Episode { return list }

func TestNextEpisode(t *testing.T) {
	season := eps(
		Episode{1, 1, "S01/e01.mkv", "Pilot"},
		Episode{1, 2, "S01/e02.mkv", "Two"},
		Episode{1, 3, "", "No file in this torrent"},
		Episode{1, 4, "S01/e04.mkv", "Four"},
		Episode{2, 1, "S02/e01.mkv", "New season"},
		Episode{0, 1, "Specials/s00e01.mkv", "Making of"},
		Episode{0, 2, "Specials/s00e02.mkv", "Bloopers"},
	)
	cases := []struct {
		name, cur, want string
	}{
		{"the one after", "S01/e01.mkv", "S01/e02.mkv"},
		{"an episode without a file is skipped, not the end", "S01/e02.mkv", "S01/e04.mkv"},
		{"the last of a season goes on to the next season", "S01/e04.mkv", "S02/e01.mkv"},
		{"the last episode of the torrent has no next", "S02/e01.mkv", ""},
		{"a special is followed by a special", "Specials/s00e01.mkv", "Specials/s00e02.mkv"},
		{"the last special does not bridge into season one", "Specials/s00e02.mkv", ""},
		{"a file that is not an episode has no next", "Extras/trailer.mkv", ""},
	}
	for _, c := range cases {
		got := NextEpisode(c.cur, season)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: want nothing, got %s", c.name, got.Path)
		case c.want != "" && (got == nil || got.Path != c.want):
			t.Errorf("%s: want %s, got %v", c.name, c.want, got)
		}
	}
	if p := NextEpisode("S01/e01.mkv", season); p.Kind != KindEpisode || p.Season != 1 || p.Episode != 2 || p.Title != "Two" {
		t.Errorf("pick carries the label data, got %+v", p)
	}
	// Regular seasons never fall back into the specials either.
	only := eps(Episode{1, 1, "a.mkv", ""}, Episode{0, 9, "sp.mkv", ""})
	if p := NextEpisode("a.mkv", only); p != nil {
		t.Errorf("S01E01 must not be followed by a special, got %s", p.Path)
	}
}

// One file, two episode rows: what follows is what comes after the LAST of
// them, and never the file itself.
func TestNextEpisodeDoubleEpisodeFile(t *testing.T) {
	list := eps(
		Episode{1, 1, "e01-02.mkv", ""},
		Episode{1, 2, "e01-02.mkv", ""},
		Episode{1, 3, "e03.mkv", ""},
	)
	p := NextEpisode("e01-02.mkv", list)
	if p == nil || p.Path != "e03.mkv" {
		t.Fatalf("want e03.mkv, got %v", p)
	}
}

// Rows come from the DB in no particular order.
func TestNextEpisodeIgnoresInputOrder(t *testing.T) {
	list := eps(Episode{1, 10, "e10.mkv", ""}, Episode{1, 2, "e02.mkv", ""}, Episode{1, 1, "e01.mkv", ""}, Episode{1, 3, "e03.mkv", ""})
	if p := NextEpisode("e02.mkv", list); p == nil || p.Path != "e03.mkv" {
		t.Fatalf("want e03.mkv, got %v", p)
	}
}

func TestNextTrack(t *testing.T) {
	files := []File{
		{Path: "Album/10 - Ten.flac", Name: "10 - Ten.flac", Audio: true},
		{Path: "Album/2 - Two.flac", Name: "2 - Two.flac", Audio: true},
		{Path: "Album/1 - One.flac", Name: "1 - One.flac", Audio: true},
		{Path: "Album/cover.jpg", Name: "cover.jpg"},
		{Path: "Album/CD2/1 - Other disc.flac", Name: "1 - Other disc.flac", Audio: true},
	}
	if p := NextTrack("Album/1 - One.flac", files); p == nil || p.Path != "Album/2 - Two.flac" {
		t.Fatalf("1 -> 2, got %v", p)
	}
	if p := NextTrack("Album/2 - Two.flac", files); p == nil || p.Path != "Album/10 - Ten.flac" {
		t.Fatalf("natural order: 2 -> 10, not 2 -> (end), got %v", p)
	} else if p.Kind != KindTrack || p.Title != "10 - Ten" {
		t.Fatalf("pick carries kind and an extension-less title, got %+v", p)
	}
	if p := NextTrack("Album/10 - Ten.flac", files); p != nil {
		t.Fatalf("the last track has no next -- and the other disc's directory is not it; got %s", p.Path)
	}
	if p := NextTrack("Album/missing.flac", files); p != nil {
		t.Fatalf("a file that is not in the listing has no next, got %s", p.Path)
	}
}

func TestNaturalLess(t *testing.T) {
	// Equal numbers fall through to the characters after them: a space
	// sorts before a dot, so "02 - x" precedes "2.mp3". Any total order will
	// do there; what matters is 2 before 10 and leading zeros not counting.
	ordered := []string{"1.mp3", "01b.mp3", "02 - x.mp3", "2.mp3", "10.mp3", "Chapter 9.mp3", "chapter 10.mp3", "z.mp3"}
	for i := 0; i+1 < len(ordered); i++ {
		if !naturalLess(ordered[i], ordered[i+1]) {
			t.Errorf("%q must sort before %q", ordered[i], ordered[i+1])
		}
		if naturalLess(ordered[i+1], ordered[i]) {
			t.Errorf("%q must not sort before %q", ordered[i+1], ordered[i])
		}
	}
	if naturalLess("a", "a") {
		t.Error("irreflexive")
	}
}
