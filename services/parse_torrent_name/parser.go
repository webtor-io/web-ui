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
