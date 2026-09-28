package discover

import (
	"testing"

	"github.com/webtor-io/web-ui/services/transcodercaps"
)

type fixedAnswer transcodercaps.Answer

func (a fixedAnswer) HEVCPassthrough() transcodercaps.Answer { return transcodercaps.Answer(a) }

// The page gets the transcoder's answer as it is, and "unknown" -- not
// "off" -- when there is no service to ask.
func TestPassthroughFor(t *testing.T) {
	var none *transcodercaps.Service
	for name, c := range map[string]struct {
		in   capabilityReader
		want string
	}{
		"no reader":           {nil, "unknown"},
		"a service never set": {none, "unknown"},
		"on":                  {fixedAnswer(transcodercaps.On), "on"},
		"off":                 {fixedAnswer(transcodercaps.Off), "off"},
		"unknown":             {fixedAnswer(transcodercaps.Unknown), "unknown"},
	} {
		if got := passthroughFor(c.in).HEVC; got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
