package user_subtitle

import (
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/language"
)

// UndeterminedLang is the BCP-47 tag for "language not known". A subtitle
// track has to carry some srclang — HTML requires it for kind="subtitles" —
// and an empty attribute is not a valid tag. "und" says the same thing
// without claiming a language the file never declared.
const UndeterminedLang = "und"

// copySuffixRe matches the duplicate-download suffix a browser or file
// manager appends before the extension: " (3)", "-3", " 3".
var copySuffixRe = regexp.MustCompile(`(?:\s*\(\d+\)|[-_ ]\d+)$`)

// LangFromName derives a subtitle's language from the extension that precedes
// its format, the convention releases already follow: "movie.en.srt" → "en".
// Anything that is not a parseable language tag (a resolution, a release tag,
// nothing at all) yields UndeterminedLang — guessing a language would mislabel
// the track in the picker and could pull it into automatic language matching.
func LangFromName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	// Browsers and file managers append " (2)" / "-2" / " 2" to a re-downloaded
	// file: "movie.ru (3).srt" is still a Russian track.
	base = copySuffixRe.ReplaceAllString(base, "")
	lc := filepath.Ext(base)
	if lc == "" {
		return UndeterminedLang
	}
	t, err := language.Parse(strings.TrimPrefix(lc, "."))
	if err != nil {
		return UndeterminedLang
	}
	return t.String()
}
