package scripts

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/webtor-io/web-ui/services/api"
)

// Where the credits begin, as the container says it.
//
// The player guesses this from subtitle timings (assets/src/js/lib/player/
// credits.js), and the first night in production that guess had something to
// work with in 11% of lookups: 78% of streams had no whole-file subtitle track
// at all. A chapter list is the file's own answer -- "End Credits" at 41:07 --
// and needs no subtitles. When both exist the chapter wins: it was put there
// by whoever made the file, not inferred.
//
// The bounds are the ones every other reader of "credits" uses
// (models.IsWatched, credits.js): between 25 s and 10 min before the end.
// A title that matches outside them is a mislabelled or differently cut file,
// and says nothing.
const (
	creditsMaxSeconds = 600
	creditsMinSeconds = 25
)

// creditsTitle matches the names encoders and releasers actually give the
// closing chapter. Anchored loosely on purpose ("Chapter 12: End Credits",
// "ED", "Ending (creditless)"), but never on a bare number or "Chapter N" --
// those say where a chapter is, not what it is.
//
// RE2's \b knows ASCII word characters only, so it cannot fence a Cyrillic
// word: those alternatives go without it (found by the test below -- "Титры"
// did not match).
var creditsTitle = regexp.MustCompile(`(?i)(\bcredits?\b|\bend\s*titles?\b|\bclosing\b|\bending\b|\boutro\b|^ed\d*$|титры|концовка|эндинг|\bgénérique\b|\babspann\b|\bcr[ée]ditos\b)`)

// creditsFromChapters returns the start of the closing credits in seconds of
// film time, or 0 when the chapters do not say.
//
// It takes the EARLIEST matching chapter inside the window: an anime episode
// has "Ending" and then "Preview", a film may have "End Credits" and then
// "Post-credits scene" -- either way the viewer's decision point is where the
// first of them starts, and they have a countdown and a Cancel from there.
func creditsFromChapters(mp *api.MediaProbe) float64 {
	if mp == nil || len(mp.Chapters) == 0 {
		return 0
	}
	duration, err := strconv.ParseFloat(mp.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return 0
	}
	best := 0.0
	for _, ch := range mp.Chapters {
		if !creditsTitle.MatchString(strings.TrimSpace(ch.Tags.Title)) {
			continue
		}
		start, err := strconv.ParseFloat(ch.StartTime, 64)
		if err != nil || start <= 0 {
			continue
		}
		if start < duration-creditsMaxSeconds || start > duration-creditsMinSeconds {
			continue
		}
		if best == 0 || start < best {
			best = start
		}
	}
	return best
}
