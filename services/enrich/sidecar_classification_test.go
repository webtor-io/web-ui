package enrich

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// TestScanSidecarFlags covers the real-world false negative: an adult
// scene release whose only marker is a non-video ".nfo" sidecar
// ("00 -XXX- 00.nfo") while the video file and folder are clean — the
// resource must still classify as adult.
func TestScanSidecarFlags(t *testing.T) {
	cases := []struct {
		name      string
		items     []ra.ListItem
		wantAdult bool
		wantSport bool
	}{
		{
			name: "xxx nfo sidecar, clean video + folder",
			items: []ra.ListItem{
				{PathStr: "/__HD____540_2026/00 -XXX- 00.nfo", MediaFormat: ra.Unknown},
				{PathStr: "/__HD____540_2026/01.mp4", MediaFormat: ra.Video},
			},
			wantAdult: true,
		},
		{
			name: "BWA34 variant — same shape",
			items: []ra.ListItem{
				{PathStr: "/BWA34___720_2026/00 -XXX- 00.nfo", MediaFormat: ra.Unknown},
				{PathStr: "/BWA34___720_2026/01.mp4", MediaFormat: ra.Video},
			},
			wantAdult: true,
		},
		{
			name: "video item carrying the marker is ignored here (covered by ptn aggregation)",
			items: []ra.ListItem{
				{PathStr: "/Some.Movie.XXX.1080p/movie.mkv", MediaFormat: ra.Video},
			},
			wantAdult: false,
		},
		{
			name: "clean release — no sidecar marker",
			items: []ra.ListItem{
				{PathStr: "/Sicario.2015.1080p/readme.txt", MediaFormat: ra.Unknown},
				{PathStr: "/Sicario.2015.1080p/sicario.mkv", MediaFormat: ra.Video},
			},
			wantAdult: false,
		},
		{
			name: "bestiality marker in a txt sidecar",
			items: []ra.ListItem{
				{PathStr: "/pack/art of zoo collection.txt", MediaFormat: ra.Unknown},
				{PathStr: "/pack/clip.mp4", MediaFormat: ra.Video},
			},
			wantAdult: true,
		},
	}
	for _, c := range cases {
		gotAdult, gotSport := scanSidecarFlags(c.items)
		if gotAdult != c.wantAdult {
			t.Errorf("%s: adult = %v, want %v", c.name, gotAdult, c.wantAdult)
		}
		if gotSport != c.wantSport {
			t.Errorf("%s: sport = %v, want %v", c.name, gotSport, c.wantSport)
		}
	}
}
