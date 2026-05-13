package parsetorrentname

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Transformer interface {
	Transform(val string) (string, error)
}

type ReplaceTransformer struct {
	replacements map[string]string
	trimSuffixes []string
	trimPrefixes []string
}

func (t *ReplaceTransformer) Transform(val string) (string, error) {
	for k, v := range t.replacements {
		val = strings.Replace(val, k, v, -1)
	}
	for _, s := range t.trimSuffixes {
		val = strings.TrimSuffix(val, s)
	}
	for _, p := range t.trimPrefixes {
		val = strings.TrimPrefix(val, p)
	}
	val = strings.TrimSpace(val)
	return val, nil
}

func NewReplaceTransformer(replacements map[string]string, trimSuffixes []string, trimPrefixes []string) *ReplaceTransformer {
	return &ReplaceTransformer{
		replacements: replacements,
		trimSuffixes: trimSuffixes,
		trimPrefixes: trimPrefixes,
	}
}

var titleTransformer = NewReplaceTransformer(map[string]string{
	"_": " ",
	".": " ",
}, []string{
	"(",
	"[",
	"-",
	"--",
	"---",
}, []string{
	")",
	"]",
	"-",
	"--",
	"---",
})

type LowercaseTransformer struct{}

func (t *LowercaseTransformer) Transform(val string) (string, error) {
	return strings.ToLower(val), nil
}

func NewLowercaseTransformer() *LowercaseTransformer {
	return &LowercaseTransformer{}
}

// MapTransformer rewrites an exact-match input value through a lookup
// table; values not in the table pass through unchanged. Used for
// alias / abbreviation normalisation — e.g. the Quality field accepts
// the "BD" prefix from "BD1080p" but stores the canonical "BluRay".
//
// Different from ReplaceTransformer (substring-replace) — Map only
// fires when the WHOLE captured content matches a key, so it can't
// accidentally rewrite a substring inside a longer field value.
type MapTransformer struct {
	m map[string]string
}

func (t *MapTransformer) Transform(val string) (string, error) {
	if v, ok := t.m[val]; ok {
		return v, nil
	}
	return val, nil
}

func NewMapTransformer(m map[string]string) *MapTransformer {
	return &MapTransformer{m: m}
}

// DateTransformer normalizes the matched date span to "YYYY-MM-DD".
// Accepts three input shapes (any of ` .-_` as separator):
//   - "YY MM DD"       — scene convention (year first, 2-digit YY → 20YY)
//   - "DD MM YYYY"     — European convention (4-digit year LAST)
//   - "YYYY MM DD"     — ISO-style (4-digit year FIRST)
// Returns the original value on parse failure so the Match drops via
// the empty-Content check would over-suppress; instead callers can
// detect non-normalized output by length.
type DateTransformer struct{}

var dateSplitRe = regexp.MustCompile(`[.\s_\-]+`)

func (t *DateTransformer) Transform(val string) (string, error) {
	parts := dateSplitRe.Split(strings.TrimSpace(val), -1)
	if len(parts) != 3 {
		return "", nil
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", nil
		}
	}
	a, _ := strconv.Atoi(parts[0])
	b, _ := strconv.Atoi(parts[1])
	c, _ := strconv.Atoi(parts[2])
	var y, m, d int
	switch {
	case len(parts[2]) == 4:
		// DD-MM-YYYY (European convention — common inside adult-release
		// parens like "(27.02.2026)").
		y, m, d = c, b, a
	case len(parts[0]) == 4:
		// YYYY-MM-DD (ISO).
		y, m, d = a, b, c
	default:
		// YY-MM-DD (scene convention). Two-digit year assumed 20YY.
		y, m, d = 2000+a, b, c
	}
	if m < 1 || m > 12 || d < 1 || d > 31 || y < 1900 || y > 2100 {
		return "", nil
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d), nil
}

func NewDateTransformer() *DateTransformer {
	return &DateTransformer{}
}
