package enrich

import (
	"regexp"
	"strings"
	"unicode"
)

// isGarbageTitle reports whether `title` is statistically unlikely to
// resolve via Claude — a pure ID, hash, release-group salt, or 4-char
// abbreviation that no metadata DB indexes.
//
// Calibrated over 4578-row ai_enrich.query corpus (2026-05-14 audit):
// composite OR of six signals catches 186 / 4578 = 4.06% of queries
// with zero recoverable false positives. All "FP-looking" cases (real
// movie path with garbage filename, e.g. /Fargo (1996)/Frgo.mkv) had
// already been tried via path-title fallback and would have produced
// the same Claude null-result.
//
// Non-ASCII scripts (CJK, Arabic, Hebrew, Cyrillic) are exempt from
// every signal here — Latin-letter heuristics don't translate, and
// Claude is genuinely useful for transliterated foreign-language
// releases that need disambiguation.
//
// Signals (all OR-composed; any one triggers):
//
//   - h1 pure digits / punctuation only
//   - h3 consonant-heavy single-token ≥8 letters, vowel ratio <15%
//   - h4 long alphanumeric run ≥12 chars with ≥30% digits (hashes, timestamps)
//   - h6 long alphanumeric token with ≥4 digit-letter transitions (random IDs)
//   - h7 single ASCII token ≥8 chars, ≥25% digits OR <20% vowels
//
// h2 (ASCII single-token <5 chars) and h5 (per-token bigram-frequency)
// were prototyped but dropped — both produced false positives on
// legitimate short or non-English titles (h2 on "M" / "Up" / "Ali",
// h5 on romanized Japanese / Tamil / Telugu). The remaining five
// signals fire only on unambiguous shape, not phonotactics or length.
func isGarbageTitle(title string) bool {
	if title == "" {
		return true
	}
	st := charStats(title)
	if st.nonAscii {
		// CJK / Arabic / Hebrew / Cyrillic — out of scope.
		return false
	}
	if h1OnlyDigits(st) {
		return true
	}
	if h3ConsonantHeavy(title, st) {
		return true
	}
	if h4HashLike(title, st) {
		return true
	}
	if h6RandomMixed(title, st) {
		return true
	}
	if h7HashSingleToken(title, st) {
		return true
	}
	return false
}

type garbageStats struct {
	latin    int
	digit    int
	vowels   int
	letters  int // latin + cyrillic
	hasSep   bool
	nonAscii bool // CJK / Arabic / Hebrew / Cyrillic — any non-ASCII letter
}

func charStats(s string) garbageStats {
	st := garbageStats{}
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF, r >= 0x3040 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7AF:
			st.nonAscii = true
		case r >= 0x0600 && r <= 0x06FF, r >= 0x0750 && r <= 0x077F, r >= 0x0590 && r <= 0x05FF:
			st.nonAscii = true
		case r >= 'а' && r <= 'я', r >= 'А' && r <= 'Я':
			st.nonAscii = true
		case r >= 'a' && r <= 'z':
			st.latin++
			st.letters++
			if strings.ContainsRune("aeiouy", r) {
				st.vowels++
			}
		case r >= 'A' && r <= 'Z':
			st.latin++
			st.letters++
			if strings.ContainsRune("AEIOUY", r) {
				st.vowels++
			}
		case unicode.IsDigit(r):
			st.digit++
		case unicode.IsLetter(r):
			// Other scripts (Hangul outside our block, Devanagari, Thai, etc.)
			st.nonAscii = true
		case r == ' ' || r == '\t' || r == '-' || r == '_' || r == '.' || r == ',':
			st.hasSep = true
		}
	}
	return st
}

func h1OnlyDigits(st garbageStats) bool {
	return st.letters == 0 && st.digit > 0
}

func h3ConsonantHeavy(_ string, st garbageStats) bool {
	if st.hasSep || st.latin < 8 {
		return false
	}
	return float64(st.vowels)/float64(st.latin) < 0.15
}

var alphanumRunLong = regexp.MustCompile(`[A-Za-z0-9]{12,}`)

func h4HashLike(s string, _ garbageStats) bool {
	for _, tok := range alphanumRunLong.FindAllString(s, -1) {
		var d, l int
		for _, r := range tok {
			if r >= '0' && r <= '9' {
				d++
			} else {
				l++
			}
		}
		tot := d + l
		if tot >= 12 && float64(d)/float64(tot) >= 0.30 {
			return true
		}
	}
	return false
}

func h6RandomMixed(s string, _ garbageStats) bool {
	for _, tok := range alphanumRunLong.FindAllString(s, -1) {
		if len(tok) < 10 {
			continue
		}
		tr := 0
		for i := 1; i < len(tok); i++ {
			a, b := tok[i-1], tok[i]
			aDig := a >= '0' && a <= '9'
			bDig := b >= '0' && b <= '9'
			if aDig != bDig {
				tr++
			}
		}
		if tr >= 4 {
			return true
		}
	}
	return false
}

func h7HashSingleToken(s string, _ garbageStats) bool {
	toks := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.' || r == ',' || r == '\t'
	})
	if len(toks) != 1 {
		return false
	}
	tok := toks[0]
	if len(tok) < 8 {
		return false
	}
	var l, d, v int
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			l++
			if strings.ContainsRune("aeiouy", r) {
				v++
			}
		case r >= 'A' && r <= 'Z':
			l++
			if strings.ContainsRune("AEIOUY", r) {
				v++
			}
		case r >= '0' && r <= '9':
			d++
		}
	}
	if l < 4 {
		return false
	}
	if float64(d)/float64(l+d) >= 0.25 {
		return true
	}
	if float64(v)/float64(l) < 0.20 {
		return true
	}
	return false
}
