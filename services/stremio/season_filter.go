package stremio

import (
	"regexp"
	"strconv"
	"strings"
)

// seasonPatterns are the ways a release names its season, in the two
// alphabets our indexers actually return. Ranges matter as much as single
// numbers: a "Сезоны 1-3" pack is a legitimate answer for season 3.
var seasonPatterns = []*regexp.Regexp{
	// S03, S3, S01E01, S01-S03, S01-03. The optional episode part matters:
	// without it "Silo.S01E01" parses as no season at all, and a wrong
	// season sails through as "unnamed".
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,2})(?:\s*-\s*s?(\d{1,2}))?(?:e\d{1,3})?(?:[^0-9a-z]|$)`),
	// Сезон 3, Сезоны 1-3, Сезон: 1-3
	regexp.MustCompile(`(?i)сезон[ы]?[:\s]*(\d{1,2})(?:\s*-\s*(\d{1,2}))?`),
	// Season 3, Seasons 1-3
	regexp.MustCompile(`(?i)season[s]?[:\s]*(\d{1,2})(?:\s*-\s*(\d{1,2}))?`),
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
	for _, re := range seasonPatterns {
		for _, m := range re.FindAllStringSubmatch(title, -1) {
			from, err := strconv.Atoi(m[1])
			if err != nil || from <= 0 {
				continue
			}
			to := from
			if len(m) > 2 && m[2] != "" {
				if t, err := strconv.Atoi(m[2]); err == nil && t >= from {
					to = t
				}
			}
			named = true
			if season >= from && season <= to {
				return true
			}
		}
	}
	return !named
}
