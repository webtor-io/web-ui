package action

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/webtor-io/web-ui/models"
)

// fallbackCount is the counter's total across its series, read from the
// default registry the service uses.
func fallbackCount(t *testing.T) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var n float64
	for _, f := range families {
		if f.GetName() != "webui_passthrough_fallback_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			n += m.GetCounter().GetValue()
		}
	}
	return n
}

// A video start that restarts a failed passthrough is counted -- and only
// that: another action, or a start without a fallback reason (every start
// but the restart), is not.
func TestNotePassthroughFallback(t *testing.T) {
	before := fallbackCount(t)
	fb := &models.VideoStreamUserData{ResourceID: "r", ItemID: "i",
		DecodeRequest: models.ParseDecodeRequest("", "decode_error", "hevc10-2160")}
	if !notePassthroughFallback("stream-video", fb) {
		t.Error("a fallback restart not noted")
	}
	for _, c := range []struct {
		action string
		vsud   *models.VideoStreamUserData
	}{
		{"stream-audio", fb},
		{"download", fb},
		{"stream-video", &models.VideoStreamUserData{ResourceID: "r", ItemID: "i", DecodeRequest: models.ParseDecodeRequest("hevc8", "", "")}},
		{"stream-video", &models.VideoStreamUserData{ResourceID: "r", ItemID: "i", DecodeRequest: models.ParseDecodeRequest("", "bogus", "hevc8")}},
		{"stream-video", nil},
	} {
		if notePassthroughFallback(c.action, c.vsud) {
			t.Errorf("%s %+v noted", c.action, c.vsud)
		}
	}
	if got := fallbackCount(t) - before; got != 1 {
		t.Errorf("counted %v, want 1", got)
	}
}
