package enrich

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/webtor-io/web-ui/services/tmdb"
)

// This file guards the metadata mappers against two related failure
// shapes that produced false-positive movie matches in production
// (e.g. the adult torrent /BWA34___720_2026/01.mp4 whose parsed title
// "01" resolved to the obscure 2026 film "0187 UFO", tmdb vote_count=0):
//
//   - isWeakSearchTitle (Layer 2) — a pre-filter that keeps part /
//     episode / disc numbers harvested from filenames ("01.mp4" -> "01")
//     out of the title search entirely. TMDB's fuzzy search resolves a
//     leading-zero numeric to an arbitrary obscure entry per year, so
//     "01"+2026 -> "0187 UFO", "01"+2025 -> a different junk film, etc.
//
//   - acceptableTMDBMatch (Layer 1) — a post-filter that rejects a TMDB
//     search result whose title doesn't actually resemble the query.
//     TMDB's /search returns substring / prefix fuzzy matches and the
//     mapper takes results[0] blindly; "01" prefix-matches "0187 UFO".
//
// The acceptance test is title-similarity ONLY. Popularity / vote_count
// is deliberately NOT consulted: a legitimately rare or brand-new film
// has few or zero votes, and gating on votes would suppress those real
// matches. A real title match resolves regardless of how obscure it is.

var titleArticles = map[string]bool{"the": true, "a": true, "an": true}

// isWeakSearchTitle reports whether a parsed title is too weak to be a
// reliable TMDB search key and should be skipped before any mapper call.
//
// The only class blocked is pure-numeric titles with a leading zero —
// these are part / episode / disc numbers lifted from filenames
// ("01.mp4", "0006.mkv"), never real movie titles. Real numeric titles
// ("1917", "300", "9", "21") never carry a leading zero, so they pass.
func isWeakSearchTitle(title string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return true
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false // contains a non-digit — not the part-number class
		}
	}
	// All digits: weak only when it carries a leading zero.
	return t[0] == '0'
}

// IsRejectableMatch reports whether a stored (parsed title → matched
// result title) metadata link would be rejected by the current
// enrichment guards. It mirrors the runtime path exactly — the
// weak-title pre-filter (searchAllMappers) plus the fuzzy-match
// post-filter (TMDB.Map) — so the cleanup command detaches precisely
// the pre-guard false positives the live pipeline would now reject and
// nothing more. resultTitle is the matched movie/series title.
func IsRejectableMatch(parsedTitle, resultTitle string) bool {
	if isWeakSearchTitle(parsedTitle) {
		return true
	}
	return !acceptableTMDBMatch(parsedTitle, &tmdb.SearchResult{Title: resultTitle})
}

// acceptableTMDBMatch reports whether a TMDB search result is a plausible
// match for the query title. It guards against TMDB's fuzzy search
// returning an obscure unrelated entry for a weak query (the "01" ->
// "0187 UFO" collision). See the file header for why vote_count is not
// part of the decision.
//
// CRITICAL: the title-alignment check fires ONLY for low-signal queries
// (see isLowSignalQuery). A confident query — a real multi-token or
// word-like title — is trusted even when the matched title shares no
// tokens with it. That is how legitimate localized / alternate-title
// matches resolve and MUST keep resolving:
//
//	"A Freira"             -> "The Nun"            (PT -> EN)
//	"La guerre des boutons" -> "War of the Buttons" (FR -> EN)
//	"Shou Trumana"         -> "The Truman Show"    (RU translit -> EN)
//	"Vecinos Invasores"    -> "Over the Hedge"     (ES -> EN)
//
// Gating these on title similarity would detach ~50% of all foreign /
// aka matches in the catalogue — the exact "don't hurt rare legit
// content" failure mode. The narrow win we want is rejecting the
// fuzzy-numeric / short-garbage collisions, nothing more.
func acceptableTMDBMatch(query string, sr *tmdb.SearchResult) bool {
	if sr == nil {
		return false
	}
	// Non-Latin queries / results (CJK, Cyrillic, Arabic, ...) use
	// different matching semantics (Kinopoisk, AI fallback) and are not
	// part of the fuzzy-numeric FP class. Leave them untouched — the
	// Latin-token heuristic below doesn't translate.
	if hasNonLatinLetter(query) || hasNonLatinLetter(sr.Title) {
		return true
	}
	// Confident query — trust the mapper's match regardless of how
	// differently it's titled (localized / aka / translated).
	if !isLowSignalQuery(query) {
		return true
	}

	q := significantTitleTokens(query)
	if len(q) == 0 {
		q = titleTokens(query)
	}
	rFull := titleTokens(sr.Title)
	if len(q) == 0 || len(rFull) == 0 {
		// Nothing comparable (e.g. punctuation-only). Be permissive —
		// isWeakSearchTitle already blocks the pure-number class.
		return true
	}

	// Squashed exact match handles compound-word splits that the token
	// comparison would miss ("Spiderman" vs "Spider-Man" -> spiderman).
	rSig := significantTitleTokens(sr.Title)
	if len(rSig) == 0 {
		rSig = rFull
	}
	if strings.Join(q, "") == strings.Join(rSig, "") {
		return true
	}

	rset := make(map[string]struct{}, len(rFull))
	for _, t := range rFull {
		rset[t] = struct{}{}
	}
	shared := 0
	for _, t := range q {
		if _, ok := rset[t]; ok {
			shared++
		}
	}
	// At least half of the query's significant tokens must appear
	// verbatim in the result title. "01" vs {0187, ufo} shares none.
	return float64(shared)/float64(len(q)) >= 0.5
}

// isLowSignalQuery reports whether a query title is weak enough that a
// non-aligning TMDB match should be distrusted. Only these queries are
// subject to the title-alignment check in acceptableTMDBMatch.
//
// Low-signal == a single token that is either pure-numeric ("01", "9"),
// very short ("R", "v34", "Up"), or shaped like release-group salt /
// hash (isGarbageTitle). Anything with two or more tokens, or a single
// real word ("Inception", "Naruto"), is a confident query and trusted.
// Note a low-signal query that genuinely matches still passes the
// alignment check ("9" -> "9", "Up" -> "Up"); only fuzzy collisions
// ("01" -> "0187 UFO", "R" -> "Dhurandhar") are rejected.
func isLowSignalQuery(query string) bool {
	toks := titleTokens(query)
	if len(toks) == 0 {
		return true
	}
	if len(toks) >= 2 {
		return false
	}
	t := toks[0]
	allDigits := true
	for _, r := range t {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return true
	}
	if len([]rune(t)) <= 3 {
		return true
	}
	return isGarbageTitle(query)
}

// titleTokens folds a title to lowercase alphanumeric tokens: diacritics
// stripped (Amélie -> amelie), punctuation collapsed to separators.
func titleTokens(s string) []string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// Combining mark (diacritic) left over from NFKD — drop it.
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// significantTitleTokens drops leading articles and bare year tokens so
// "The Matrix" compares as {matrix} and "Sicario 2015" as {sicario}.
func significantTitleTokens(s string) []string {
	toks := titleTokens(s)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if titleArticles[t] || isYearToken(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// isYearToken reports whether a token is a bare 4-digit year (1900-2099).
func isYearToken(t string) bool {
	if len(t) != 4 {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false
		}
	}
	return t >= "1900" && t <= "2099"
}

// hasNonLatinLetter reports whether s contains a letter outside the Latin
// script (CJK, Cyrillic, Arabic, ...). Accented Latin letters (é, ü) are
// Latin-script and do NOT count — they're folded by titleTokens.
func hasNonLatinLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}
