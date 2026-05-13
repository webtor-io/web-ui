package parsetorrentname

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Match struct {
	FieldType FieldType
	Start     int
	End       int
	Raw       string
	Content   string
	// Transient matches are detected and reported (so the matched
	// FieldType still gets set on the output struct) but their
	// (Start, End) span is NOT subtracted from the input available
	// to subsequent matchers. Used for "flag-only" fields like Adult
	// where we want the side-effect (boolean=true) without ripping
	// the matched keyword out of the title — "Bang My Tranny Ass"
	// should still produce Title="Bang My Tranny Ass" + Adult=true,
	// not Title="Bang My" because `tranny` was consumed mid-string.
	Transient bool
}

type Matches []*Match

func (s Matches) getAvailable(start int, end int) (int, int, bool) {
	// Iterate matches sorted by Start so the left-to-right shrinking
	// of (start, end) is monotonic. Without sorting, a Studio match at
	// position 0 arriving LATER in the slice could coincide with an
	// already-shrunken range and trigger the proper-subset
	// short-circuit below, returning ok=false even though a real
	// available subrange exists elsewhere. Insertion sort on a copy —
	// N is small (handful of fields per filename) and we must not
	// mutate the caller's slice order.
	live := make([]*Match, 0, len(s))
	for _, m := range s {
		if m.Transient {
			continue
		}
		live = append(live, m)
	}
	for i := 1; i < len(live); i++ {
		for j := i; j > 0 && live[j].Start < live[j-1].Start; j-- {
			live[j], live[j-1] = live[j-1], live[j]
		}
	}
	for _, m := range live {
		if m.Start <= start && m.End >= end {
			return start, end, false
		}
		if m.End > start && m.End <= end && m.Start <= start {
			start = m.End
		}
		if m.Start > start && m.Start <= end {
			end = m.Start
		}
	}
	return start, end, start < end
}

type Matcher interface {
	Match(ftype FieldType, input string, matches Matches) (*Match, error)
}

type RegexpMatcher struct {
	reArr []*regexp.Regexp
	last  bool
}

func (s *RegexpMatcher) matchRe(ftype FieldType, re *regexp.Regexp, input string, matches Matches) *Match {
	ms := re.FindAllStringSubmatch(input, -1)
	if len(ms) == 0 {
		return nil
	}
	mms := Matches{}
	var start, end, nStart, nEnd, cStart, cEnd int
	var ok bool
	for _, m := range ms {
		start = strings.Index(input, m[1])
		end = start + len(m[1])
		nStart, nEnd, ok = matches.getAvailable(start, end)
		if !ok {
			continue
		}
		content := m[2]
		cStart = strings.Index(m[1], content)
		if cStart < nStart-start {
			cStart = nStart - start
		}
		cEnd = strings.Index(m[1], content) + len(content)
		if cEnd > nEnd-start {
			cEnd = nEnd - start
		}
		content = m[1][cStart:cEnd]
		content = strings.Trim(content, ".")
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		rStart := nStart - start
		rEnd := nEnd - end + len(m[1])
		raw := m[1][rStart:rEnd]
		mms = append(mms, &Match{
			FieldType: ftype,
			Start:     nStart,
			End:       nEnd,
			Content:   content,
			Raw:       raw,
		})
	}
	if len(mms) == 0 {
		return nil
	}
	matchIdx := 0
	if s.last {
		matchIdx = len(mms) - 1
	}
	return mms[matchIdx]
}

func (s *RegexpMatcher) Match(ftype FieldType, input string, matches Matches) (*Match, error) {
	var m *Match
	for _, re := range s.reArr {
		m = s.matchRe(ftype, re, input, matches)
		if m != nil {
			break
		}
	}
	return m, nil
}

func NewRegexpMatcher(reStrArr ...string) *RegexpMatcher {
	var reArr []*regexp.Regexp

	for _, reStr := range reStrArr {
		re := regexp.MustCompile(reStr)
		if re.NumSubexp() != 2 {
			fmt.Printf("Pattern %q does not have enough capture groups. want 2, got %d\n", reStr, re.NumSubexp())
			os.Exit(1)
		}
		reArr = append(reArr, re)
	}
	return &RegexpMatcher{
		reArr: reArr,
	}
}
func NewRegexpMatcherLast(reStrArr ...string) *RegexpMatcher {
	m := NewRegexpMatcher(reStrArr...)
	m.last = true
	return m
}

var _ Matcher = (*RegexpMatcher)(nil)
