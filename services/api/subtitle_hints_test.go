package api

import "testing"

func TestWithSubtitleHints(t *testing.T) {
	base := "https://x.test/abc/Movie.mkv~vi/subtitles.json?token=T&api-key=K"
	cases := []struct {
		name string
		h    SubtitleHints
		want string
	}{
		{"empty", SubtitleHints{}, base},
		{"imdb only", SubtitleHints{ImdbID: "tt0109424"}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&imdb-id=tt0109424&token=T"},
		{"episode", SubtitleHints{ImdbID: "tt0903747", Season: 1, Episode: 3}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&episode=3&imdb-id=tt0903747&season=1&token=T"},
		{"season without episode ignored", SubtitleHints{ImdbID: "tt1", Season: 2}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&imdb-id=tt1&token=T"},
	}
	for _, c := range cases {
		if got := WithSubtitleHints(base, c.h); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestWithSubtitleHintsKeepsExistingImdb(t *testing.T) {
	base := "https://x.test/a/b~vi/subtitles.json?imdb-id=tt9&token=T"
	got := WithSubtitleHints(base, SubtitleHints{ImdbID: "tt1"})
	if got != "https://x.test/a/b~vi/subtitles.json?imdb-id=tt9&token=T" {
		t.Errorf("rest-api supplied imdb-id must win: %q", got)
	}
}
