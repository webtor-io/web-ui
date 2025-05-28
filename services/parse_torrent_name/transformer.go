package parsetorrentname

import "strings"

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
