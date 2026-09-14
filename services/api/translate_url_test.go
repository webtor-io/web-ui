package api

import "testing"

func TestTranslateURL(t *testing.T) {
	cases := []struct {
		src, lang string
		names     []string
		want      string
	}{
		{"https://x.test/abc/Dir/movie.srt~vtt/movie.vtt?token=T&api-key=K", "pt", nil,
			"https://x.test/abc/Dir/movie.srt~vtt/movie.vtt~tr:pt/movie.vtt?token=T&api-key=K"},
		{"https://x.test/abc/movie.mkv~vi/opensubtitles/123.vtt?token=T", "ru", []string{"Hildy", "Walter"},
			"https://x.test/abc/movie.mkv~vi/opensubtitles/123.vtt~tr:ru/123.vtt?token=T&names=Hildy%2CWalter"},
		{"https://x.test/ext/aGVsbG8%3D%3D/user.srt~vtt/user.vtt?token=T", "es", nil,
			"https://x.test/ext/aGVsbG8%3D%3D/user.srt~vtt/user.vtt~tr:es/user.vtt?token=T"},
		{"", "pt", nil, ""},
		// A src with no scheme/host cannot be turned into a URL the proxy
		// can fetch: parsed.Scheme + "://" + parsed.Host would produce
		// "://path/x~tr:pt/x.vtt", a string that looks like a URL and is
		// not one. No item is better than an item that 404s.
		{"/ext/abc/movie.srt~vtt/movie.vtt?token=T", "pt", nil, ""},
		{"movie.srt~vtt/movie.vtt", "pt", nil, ""},
		{"//x.test/abc/movie.vtt", "pt", nil, ""},
		{"https:///abc/movie.vtt", "pt", nil, ""},
	}
	for _, c := range cases {
		if got := TranslateURL(c.src, c.lang, c.names); got != c.want {
			t.Errorf("TranslateURL(%q,%q,%v)\n got %q\nwant %q", c.src, c.lang, c.names, got, c.want)
		}
	}
}
