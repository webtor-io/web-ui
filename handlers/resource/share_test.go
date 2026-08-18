package resource

import (
	"strings"
	"testing"
)

func TestResolveSharePath(t *testing.T) {
	const hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	tests := []struct {
		name     string
		title    string
		text     string
		url      string
		ok       bool
		contains []string
	}{
		{
			name:     "magnet in text",
			text:     "magnet:?xt=urn:btih:" + hash + "&dn=Sintel",
			ok:       true,
			contains: []string{hash},
		},
		{
			name:     "magnet surrounded by text",
			text:     "check this out magnet:?xt=urn:btih:" + hash + "&dn=Sintel and enjoy",
			ok:       true,
			contains: []string{hash},
		},
		{
			name:     "magnet in url field",
			url:      "magnet:?xt=urn:btih:" + hash,
			ok:       true,
			contains: []string{hash},
		},
		{
			name:     "magnet in title",
			title:    "magnet:?xt=urn:btih:" + hash,
			ok:       true,
			contains: []string{hash},
		},
		{
			name:     "bare infohash in text",
			text:     "Sintel " + hash + " torrent",
			ok:       true,
			contains: []string{hash},
		},
		{
			name:     "magnet keeps trackers",
			text:     "magnet:?xt=urn:btih:" + hash + "&tr=udp%3A%2F%2Ftracker.example%3A6969",
			ok:       true,
			contains: []string{hash, "tracker.example"},
		},
		{
			name: "no torrent reference",
			text: "just some random shared text",
			url:  "https://example.com/page",
			ok:   false,
		},
		{
			// Short hex runs inside ordinary URLs must not resolve: the
			// lenient search-box heuristic (sv.SHA1R {5,40}) turned
			// "facebook" into btih:faceb.
			name: "shared url with incidental hex run",
			url:  "https://www.facebook.com/story/123",
			ok:   false,
		},
		{
			name: "torrent site url with numeric id",
			url:  "https://1337x.to/torrent/1234567/Sintel/",
			ok:   false,
		},
		{
			// A 68-hex btmh digest must not be truncated to its first 40
			// chars — that yields a valid-looking but nonexistent v1 hash.
			name: "v2-only magnet mid-string",
			text: "look at this magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e wow",
			ok:   false,
		},
		{
			name:     "infohash inside a url path",
			url:      "https://example.com/torrent/" + hash + "/details",
			ok:       true,
			contains: []string{hash},
		},
		{
			name: "empty input",
			ok:   false,
		},
		{
			name: "v2-only magnet rejected",
			text: "magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e",
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveSharePath(tt.title, tt.text, tt.url)
			if ok != tt.ok {
				t.Fatalf("resolveSharePath() ok = %v, want %v (got %q)", ok, tt.ok, got)
			}
			if !ok {
				return
			}
			if !strings.HasPrefix(got, "/magnet:?") {
				t.Fatalf("resolveSharePath() = %q, want path starting with /magnet:?", got)
			}
			for _, sub := range tt.contains {
				if !strings.Contains(got, sub) {
					t.Errorf("resolveSharePath() = %q, want it to contain %q", got, sub)
				}
			}
		})
	}
}
