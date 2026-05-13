package parsetorrentname

import "strings"

type Parser interface {
	Parse(input string, matches Matches) (Matches, error)
}

type FieldParser struct {
	FieldType   FieldType
	Matcher     Matcher
	Transformer Transformer
}

func (s *FieldParser) Parse(input string, matches Matches) (Matches, error) {
	m, err := s.Matcher.Match(s.FieldType, input, matches)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	if s.Transformer != nil {
		v, err := s.Transformer.Transform(m.Content)
		if err != nil {
			return nil, err
		}
		if v == "" {
			return nil, nil
		}
		m.Content = v
	}
	return Matches{m}, nil
}

// TransientFieldParser wraps a FieldParser so the produced Match is
// flagged Transient — detected and reported (the field gets set) but
// its (Start, End) span is NOT subtracted from the input available to
// downstream parsers. Used for flag-only fields like Adult where the
// matched keyword sitting mid-title would otherwise truncate Title.
type TransientFieldParser struct {
	inner Parser
}

func (s *TransientFieldParser) Parse(input string, matches Matches) (Matches, error) {
	ms, err := s.inner.Parse(input, matches)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		m.Transient = true
	}
	return ms, nil
}

func NewTransientFieldParser(ftype FieldType, matcher Matcher, transformer Transformer) Parser {
	return &TransientFieldParser{
		inner: NewFieldParser(ftype, matcher, transformer),
	}
}

// SeparatorExpander mutates accumulated non-transient matches IN PLACE
// to swallow adjacent separator characters (`. _-` plus whitespace).
// Run AFTER all field parsers but BEFORE the Title scope so the gaps
// between consumed spans become non-substantive — otherwise the Title
// regex `[^\[\(\{]*` ends up with a "first available subrange" that's
// just a stray dot ("angelslove" + "." + "26.05.10" → Title="." or "").
//
// Boundaries: a span's End is allowed to grow until it touches the
// next non-transient match's Start, and Start can shrink down to the
// previous match's End (or 0 / len(input) at the edges). Transient
// matches are ignored — they don't claim space.
type SeparatorExpander struct{}

func isSeparatorByte(b byte) bool {
	return b == '.' || b == ' ' || b == '_' || b == '-' || b == '\t'
}

func (e *SeparatorExpander) Parse(input string, matches Matches) (Matches, error) {
	// Collect non-transient matches with valid spans, sorted by Start.
	live := make([]*Match, 0, len(matches))
	for _, m := range matches {
		if m.Transient || m.Start >= m.End {
			continue
		}
		live = append(live, m)
	}
	// Insertion sort — N is small (handful of fields per filename).
	for i := 1; i < len(live); i++ {
		for j := i; j > 0 && live[j].Start < live[j-1].Start; j-- {
			live[j], live[j-1] = live[j-1], live[j]
		}
	}
	for i, m := range live {
		floor := 0
		if i > 0 {
			floor = live[i-1].End
		}
		ceiling := len(input)
		if i+1 < len(live) {
			ceiling = live[i+1].Start
		}
		for m.End < ceiling && isSeparatorByte(input[m.End]) {
			m.End++
		}
		for m.Start > floor && isSeparatorByte(input[m.Start-1]) {
			m.Start--
		}
		// Eat paired wrapping brackets that are otherwise abandoned
		// between consumed spans. Year=2025 inside "(2025)" used to
		// leave the parens behind, which then formed orphan available
		// subranges like "(" → Group regex captured the stray paren.
		// Only swallow when BOTH sides have the same bracket pair —
		// half-open brackets stay as title delimiters.
		if m.Start > floor && m.End < ceiling {
			lb, rb := input[m.Start-1], input[m.End]
			pair := (lb == '(' && rb == ')') ||
				(lb == '[' && rb == ']') ||
				(lb == '{' && rb == '}')
			if pair {
				m.Start--
				m.End++
			}
		}
	}
	return nil, nil
}

func NewSeparatorExpander() Parser {
	return &SeparatorExpander{}
}

// ExtraExtractor runs LAST in the parser chain and emits FieldTypeExtra
// containing any input bytes that no other parser (including the
// greedy Title) consumed. Typical sources:
//
//   - paren-wrapped tags the Title regex skipped over: "(Legendado)",
//     "(Original Sub)", "(DUB)"
//   - dropped release notes like "[XC]" when Group claimed something
//     else
//   - language hints, fansub group annotations, etc.
//
// Bracket characters and pure-separator runs are stripped; remaining
// text fragments are joined by a single space.
//
// The output match is Transient — it's a SUMMARY field, it doesn't
// claim space against any other parser. Runs after Group at the
// chain tail.
type ExtraExtractor struct{}

func (e *ExtraExtractor) Parse(input string, matches Matches) (Matches, error) {
	type span struct{ s, e int }
	var consumed []span
	for _, m := range matches {
		if m.Transient || m.Start >= m.End {
			continue
		}
		consumed = append(consumed, span{m.Start, m.End})
	}
	for i := 1; i < len(consumed); i++ {
		for j := i; j > 0 && consumed[j].s < consumed[j-1].s; j-- {
			consumed[j], consumed[j-1] = consumed[j-1], consumed[j]
		}
	}
	// Merge overlapping spans so the walk below sees clean intervals.
	merged := consumed[:0]
	for _, c := range consumed {
		if len(merged) > 0 && c.s <= merged[len(merged)-1].e {
			if c.e > merged[len(merged)-1].e {
				merged[len(merged)-1].e = c.e
			}
			continue
		}
		merged = append(merged, c)
	}
	var parts []string
	pos := 0
	emit := func(start, end int) {
		if start >= end {
			return
		}
		frag := cleanExtraFragment(input[start:end])
		if frag != "" {
			parts = append(parts, frag)
		}
	}
	for _, c := range merged {
		emit(pos, c.s)
		pos = c.e
	}
	emit(pos, len(input))
	if len(parts) == 0 {
		return nil, nil
	}
	return Matches{&Match{
		FieldType: FieldTypeExtra,
		Content:   strings.Join(parts, " "),
		Transient: true,
	}}, nil
}

// cleanExtraFragment normalises a leftover string into something
// readable: strip surrounding brackets, collapse separator runs into
// spaces, trim, and reject fragments that are pure punctuation. The
// trim set is intentionally generous — single-symbol leftovers like
// "/" between consumed spans of "(CamRip / 2014)" are noise, not
// metadata.
const extraTrimChars = " \t.,_-/&+|:;~"

func cleanExtraFragment(s string) string {
	s = strings.Trim(s, extraTrimChars)
	// Strip ONE level of wrapping brackets if both sides match.
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '(' && last == ')') ||
			(first == '[' && last == ']') ||
			(first == '{' && last == '}') {
			s = s[1 : len(s)-1]
			s = strings.TrimSpace(s)
		}
	}
	// Drop interior brackets / replace separators with spaces.
	s = strings.NewReplacer(
		".", " ",
		"_", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		"/", " ",
	).Replace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	// Reject fragments that became empty or are just punctuation.
	if s == "" || strings.Trim(s, extraTrimChars) == "" {
		return ""
	}
	return s
}

func NewExtraExtractor() Parser {
	return &ExtraExtractor{}
}

var _ Parser = (*FieldParser)(nil)

type CompoundParser struct {
	parsers []Parser
}

func (c CompoundParser) Parse(input string, matches Matches) (Matches, error) {
	var localMatches Matches
	for _, p := range c.parsers {
		inputMatches := Matches{}
		for _, m := range matches {
			inputMatches = append(inputMatches, m)
		}
		for _, m := range localMatches {
			inputMatches = append(localMatches, m)
		}
		ms, err := p.Parse(input, inputMatches)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			localMatches = append(localMatches, m)
		}
	}
	return localMatches, nil
}

func NewCompoundParser(parsers []Parser) *CompoundParser {
	return &CompoundParser{
		parsers: parsers,
	}
}

type ScopeParser struct {
	parser  Parser
	matcher Matcher
}

func NewScopeParser(matcher Matcher, parser Parser) Parser {
	return &ScopeParser{
		matcher: matcher,
		parser:  parser,
	}
}

func (s *ScopeParser) Parse(input string, matches Matches) (Matches, error) {
	sm, err := s.matcher.Match(FieldTypeUnknown, input, matches)
	if err != nil {
		return nil, err
	}
	if sm == nil {
		return nil, nil
	}
	mms, err := s.parser.Parse(sm.Content, Matches{})
	if err != nil {
		return nil, err
	}
	for _, m := range mms {
		m.Start += sm.Start
		m.End += sm.Start
	}
	mms = append(mms, sm)
	return mms, nil
}

func NewFieldParser(ftype FieldType, matcher Matcher, transformer Transformer) Parser {
	return &FieldParser{
		FieldType:   ftype,
		Matcher:     matcher,
		Transformer: transformer,
	}
}

type FieldParsers []*FieldParser

func (s FieldParsers) ToParserSlice() []Parser {
	var out []Parser
	for _, fp := range s {
		out = append(out, fp)
	}
	return out
}
