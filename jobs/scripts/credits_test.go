package scripts

import (
	"testing"

	"github.com/webtor-io/web-ui/services/api"
)

func probe(duration string, chapters ...[2]string) *api.MediaProbe {
	mp := &api.MediaProbe{}
	mp.Format.Duration = duration
	for _, c := range chapters {
		ch := api.MediaProbeChapter{StartTime: c[0]}
		ch.Tags.Title = c[1]
		mp.Chapters = append(mp.Chapters, ch)
	}
	return mp
}

func TestCreditsFromChapters(t *testing.T) {
	cases := []struct {
		name string
		mp   *api.MediaProbe
		want float64
	}{
		{"a film's own answer", probe("2700.0", [2]string{"0.0", "Opening"}, [2]string{"120.0", "Episode"}, [2]string{"2467.5", "End Credits"}), 2467.5},
		{"anime: the ending, not the preview after it", probe("1440.0", [2]string{"0", "OP"}, [2]string{"90", "Part A"}, [2]string{"1310", "ED"}, [2]string{"1400", "Preview"}), 1310},
		{"numbered title with a name in it", probe("2700", [2]string{"2500", "Chapter 12: End Credits"}), 2500},
		{"russian release", probe("2700", [2]string{"2480", "Титры"}), 2480},
		{"generic chapter names say where, not what", probe("2700", [2]string{"0", "Chapter 1"}, [2]string{"2500", "Chapter 12"}), 0},
		{"a match twenty minutes before the end is another cut, not credits", probe("2700", [2]string{"1500", "Credits"}), 0},
		{"a match in the last seconds gains nothing", probe("2700", [2]string{"2690", "Credits"}), 0},
		{"opening credits are not closing credits: they are not in the window", probe("2700", [2]string{"30", "Opening Credits"}), 0},
		{"credits, then a post-credits scene: the first decision point", probe("7200", [2]string{"6800", "End Credits"}, [2]string{"7100", "Post-credits scene"}), 6800},
		{"no chapters (an older cached probe)", probe("2700"), 0},
		{"unknown duration", probe("", [2]string{"2500", "Credits"}), 0},
		{"nil probe", nil, 0},
	}
	for _, c := range cases {
		if got := creditsFromChapters(c.mp); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// Words that contain "ed" or "ending" inside something else must not match.
func TestCreditsTitleIsNotGreedy(t *testing.T) {
	for _, title := range []string{"Wedding", "The Red Room", "Edinburgh", "Chapter 3", "Bedtime", "Mended"} {
		if creditsTitle.MatchString(title) {
			t.Errorf("%q must not read as credits", title)
		}
	}
	for _, title := range []string{"ED", "ED2", "Ending", "End Titles", "Closing", "Outro", "Credits", "credit roll"} {
		if !creditsTitle.MatchString(title) {
			t.Errorf("%q must read as credits", title)
		}
	}
}
