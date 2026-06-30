package stremio

import (
	"context"
	"testing"
)

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1503238553, "1.40 GB"}, // ~1.4 GiB
		{12 * 1024 * 1024 * 1024, "12.00 GB"},
		{int64(13_217_000_000), "12.31 GB"},
		{150 * 1024 * 1024, "150 MB"}, // ≥100 → no decimals
	}
	for _, c := range cases {
		if got := humanizeBytes(c.n); got != c.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestLanguageFlags(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"The.Big.Bang.Theory.S05E14.1080p.BluRay.Rus.Eng.TeamHD.mkv", "🇷🇺 / 🇬🇧"}, // first-seen order
		{"Project.Hail.Mary.2026.1080p.WEB-DL.mkv", ""},                           // no lang token
		{"Some.Movie.RUS.RUS.mkv", "🇷🇺"},                                          // de-duplicated
		{"Film.Eng.Ita.Fre.mkv", "🇬🇧 / 🇮🇹 / 🇫🇷"},
	}
	for _, c := range cases {
		if got := languageFlags(c.name); got != c.want {
			t.Errorf("languageFlags(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func i16(v int16) *int16 { return &v }
func i64(v int64) *int64 { return &v }

func TestMakeStreamTitle(t *testing.T) {
	cases := []struct {
		name       string
		displayTtl string
		ct         string
		args       *Args
		md         map[string]any
		size       *int64
		filename   string
		year       *int16
		want       string
	}{
		{
			name:       "series full attributes",
			displayTtl: "The Big Bang Theory",
			ct:         "series",
			args:       &Args{Season: 5, Episode: 14},
			md:         map[string]any{"year": float64(2012), "quality": "BluRay", "resolution": "1080p"},
			size:       i64(1503238553),
			filename:   "The.Big.Bang.Theory.S05E14.1080p.BluRay.Rus.Eng.TeamHD.mkv",
			want:       "The Big Bang Theory · S05E14 [2012 BluRay 1080p]\n💾 1.40 GB  ⚙️ Library\n🇷🇺 / 🇬🇧",
		},
		{
			name:       "movie full attributes",
			displayTtl: "Project Hail Mary",
			ct:         "movie",
			args:       &Args{},
			md:         map[string]any{"quality": "WEB-DL", "resolution": "1080p"},
			size:       i64(int64(13_217_000_000)),
			filename:   "Project.Hail.Mary.2026.1080p.WEB-DL.Rus.Eng.Ger.mkv",
			year:       i16(2026),
			want:       "Project Hail Mary [2026 WEB-DL 1080p]\n💾 12.31 GB  ⚙️ Library\n🇷🇺 / 🇬🇧 / 🇩🇪",
		},
		{
			name:       "no metadata at all",
			displayTtl: "Mystery File",
			ct:         "movie",
			args:       &Args{},
			md:         nil,
			size:       nil,
			filename:   "mystery.mkv",
			want:       "Mystery File\n⚙️ Library",
		},
		{
			name:       "resolution only, no size, no lang",
			displayTtl: "Some Show",
			ct:         "series",
			args:       &Args{Season: 1, Episode: 3},
			md:         map[string]any{"resolution": "720p"},
			size:       nil,
			filename:   "some.show.s01e03.720p.mkv",
			want:       "Some Show · S01E03 [720p]\n⚙️ Library",
		},
		{
			name:       "year from content fallback when absent in md",
			displayTtl: "Old Movie",
			ct:         "movie",
			args:       &Args{},
			md:         map[string]any{"quality": "BluRay"},
			size:       i64(700 * 1024 * 1024),
			filename:   "old.movie.bluray.mkv",
			year:       i16(1999),
			want:       "Old Movie [1999 BluRay]\n💾 700 MB  ⚙️ Library",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := makeStreamTitle(c.displayTtl, c.ct, c.args, c.md, c.size, c.filename, c.year)
			if got != c.want {
				t.Errorf("makeStreamTitle:\n got = %q\nwant = %q", got, c.want)
			}
		})
	}
}

// When FileIdx was persisted at enrich time, resolveFileItem must return it
// verbatim with the filename derived from the path basename — and must NOT
// touch rest-api (sapi is nil here, so any list call would panic). This is
// the fast path that removes per-stream rest-api round-trips from /stream.
func TestLibrary_resolveFileItem_PersistedIdxSkipsRestAPI(t *testing.T) {
	l := NewLibrary("https://webtor.io", nil, nil, nil, nil)

	idx := 100
	p := "/The.Big.Bang.Theory/S05/The.Big.Bang.Theory.S05E14.1080p.mkv"

	gotIdx, gotName, err := l.resolveFileItem(context.Background(), "deadbeef", p, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIdx != 100 {
		t.Errorf("idx = %d, want 100", gotIdx)
	}
	if gotName != "The.Big.Bang.Theory.S05E14.1080p.mkv" {
		t.Errorf("name = %q, want basename of path", gotName)
	}
}

// FileIdx == 0 is a valid index (the first file in the torrent), not an
// "unset" sentinel — a non-nil pointer to 0 must still take the fast path.
func TestLibrary_resolveFileItem_ZeroIdxIsValid(t *testing.T) {
	l := NewLibrary("https://webtor.io", nil, nil, nil, nil)

	idx := 0
	gotIdx, gotName, err := l.resolveFileItem(context.Background(), "deadbeef", "/movie.mkv", &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIdx != 0 {
		t.Errorf("idx = %d, want 0", gotIdx)
	}
	if gotName != "movie.mkv" {
		t.Errorf("name = %q, want movie.mkv", gotName)
	}
}
