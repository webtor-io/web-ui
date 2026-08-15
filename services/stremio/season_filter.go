package stremio

import (
	"regexp"
	"strconv"
	"strings"
)

// seasonRange is one season reading of a title: the closed interval it names.
type seasonRange struct{ from, to int }

// covers reports whether the reading answers a request for this season.
func (r seasonRange) covers(season int) bool {
	return season >= r.from && season <= r.to
}

// seasonPatterns are the abbreviated spellings, where the number always
// follows the marker. The spelled-out ones live in wordySeasons below, which
// needs a different rule. Ranges matter as much as single numbers: an
// "S01-S03" pack is a legitimate answer for season 3.
var seasonPatterns = []*regexp.Regexp{
	// S03, S3, S01E01, S01-S03, S01-03. The optional episode part matters:
	// without it "Silo.S01E01" parses as no season at all, and a wrong
	// season sails through as "unnamed".
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,2})(?:\s*-\s*s?(\d{1,2}))?(?:e\d{1,3})?(?:[^0-9a-z]|$)`),
}

// wordySeason reads a spelling where the number can sit on either side of the
// word "season", and the side decides what the numbers on the other side mean.
//
//	Сезон: 1 / Серии: 1-10   → season 1   (rutracker's canonical title)
//	Сезоны: 1-3              → seasons 1-3
//	3 сезон: 1-8 серии       → season 3   (RuTor/Kinozal/NNM word order)
//	1-7 сезоны               → seasons 1-7
//	10 сезонов               → seasons 1-10, a count, not season 10
//	Season 3 / 9 seasons     → season 3 / seasons 1-9
//
// Both word orders have to be read by one pattern rather than two, because
// the numbers after the word are episodes when a number precedes it —
// matching them separately made "[3 сезон: 1-8 серии]" parse as seasons 1-8
// and pass a request for season 1.
//
// Groups: 1,2 = the range before the word, 3 = the word's suffix, 4,5 = the
// range after it.
type wordySeason struct {
	re *regexp.Regexp
	// countSuffixes are the inflections that turn a preceding number into a
	// total rather than an ordinal: "10 сезонов", "3 сезона", "9 seasons".
	countSuffixes map[string]bool
}

// wordySeasons are the two alphabets' spellings. The leading (?:^|[^0-9])
// keeps a year from being read as a season in "Silo 2023 сезон".
var wordySeasons = []wordySeason{
	{
		re: regexp.MustCompile(
			`(?i)(?:(?:^|[^0-9])(\d{1,2})(?:\s*-\s*(\d{1,2}))?\s*-?\s*(?:й\s*)?)?сезон(ов|ы|а|)[:\s]*(?:(\d{1,2})(?:\s*-\s*(\d{1,2}))?)?`),
		countSuffixes: map[string]bool{"ов": true, "а": true},
	},
	{
		re: regexp.MustCompile(
			`(?i)(?:(?:^|[^0-9])(\d{1,2})(?:\s*-\s*(\d{1,2}))?\s*)?season(s|)[:\s]*(?:(\d{1,2})(?:\s*-\s*(\d{1,2}))?)?`),
		countSuffixes: map[string]bool{"s": true},
	},
}

// seasonsFromWords returns every season reading of the spelled-out forms in a
// title.
func seasonsFromWords(title string) []seasonRange {
	var out []seasonRange
	for _, w := range wordySeasons {
		for _, m := range w.re.FindAllStringSubmatch(title, -1) {
			before, beforeTo, suffix, after, afterTo := m[1], m[2], strings.ToLower(m[3]), m[4], m[5]
			switch {
			case before != "" && beforeTo == "" && w.countSuffixes[suffix]:
				// A count, not an ordinal: a complete-series pack covering
				// 1..N. Read as season N it would be dropped from every
				// request but the last season's.
				if n := atoiPositive(before); n > 0 {
					out = append(out, seasonRange{1, n})
				}
			case before != "":
				// The number before the word wins: whatever follows it is
				// the episode range, not a season one.
				out = append(out, rangeOf(before, beforeTo))
			case after != "":
				out = append(out, rangeOf(after, afterTo))
			}
		}
	}
	return out
}

// rangeOf builds a reading from a "from" and an optional "to", dropping a
// reversed pair rather than inventing an empty interval.
func rangeOf(from, to string) seasonRange {
	f := atoiPositive(from)
	r := seasonRange{f, f}
	if t := atoiPositive(to); t >= f {
		r.to = t
	}
	return r
}

func atoiPositive(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// matchesRequestedSeason reports whether a release title is a plausible
// answer for the requested season.
//
// It is deliberately permissive: a title that names no season at all — a
// bare "Silo.S03E01.1080p" is caught by the first pattern, but plenty of
// releases name nothing — is kept, because the alternative is dropping good
// results on a parsing miss. Only a title that names a season, and names a
// different one, is rejected.
//
// This exists because an indexer that advertises season/ep in its caps does
// not necessarily honour them: several Jackett definitions fall back to a
// plain keyword search, and the user then sees season one of a series they
// asked the third season of.
func matchesRequestedSeason(title string, season int) bool {
	if season <= 0 || strings.TrimSpace(title) == "" {
		return true
	}
	named := false
	for _, r := range seasonsFromWords(title) {
		if r.from <= 0 {
			continue
		}
		named = true
		if r.covers(season) {
			return true
		}
	}
	for _, re := range seasonPatterns {
		for _, m := range re.FindAllStringSubmatch(title, -1) {
			r := rangeOf(m[1], m[2])
			if r.from <= 0 {
				continue
			}
			named = true
			if r.covers(season) {
				return true
			}
		}
	}
	return !named
}
