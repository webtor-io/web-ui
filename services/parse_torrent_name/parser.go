package parsetorrentname

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
// downstream parsers. Used for flag-only fields like Porn where the
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
