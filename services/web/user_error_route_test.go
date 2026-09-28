package web

import (
	"net/http"
	"testing"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
)

const over1080 = "resolution over 1080p is not supported\n"

// refused is a refusal in the shape the stream job hands the error
// formatter: the api client's typed error, wrapped by hls.go and action.go.
func refused(status int, body, reason string, fallback bool) error {
	return errors.Wrap(errors.Wrap(&api.TranscoderRefusal{Status: status, Body: body, Reason: reason, Fallback: fallback},
		"failed to create transcoder session"), "failed to buffer session HLS")
}

// Each route reason gets the text that says why this 4K file is not shown
// here (docs/user_errors.md, "Route refusals"). Every key exists in English
// (the parity test holds the other locales to it).
func TestClassifyError_RouteRefusal(t *testing.T) {
	en := englishMessages(t)
	for _, c := range []struct {
		reason string
		want   string
	}{
		{"declaration_pending", "error.video_route.checking"},
		{"needs_2160", "error.video_route.needs_uhd_hevc"},
		{"needs_main10", "error.video_route.needs_uhd_hevc"},
		{"needs_main", "error.video_route.needs_uhd_hevc"},
		{"needs_high_tier", "error.video_route.needs_uhd_hevc"},
		{"needs_pq", "error.video_route.needs_pq"},
		{"too_large", "error.video_route.too_large"},
		{"dv5", "error.video_route.unsafe_format"},
		{"dv7", "error.video_route.unsafe_format"},
		{"dv_base", "error.video_route.unsafe_format"},
		{"dv_unknown", "error.video_route.unsafe_format"},
		{"pix_fmt", "error.video_route.unsafe_format"},
		{"profile", "error.video_route.unsafe_format"},
		{"interlaced", "error.video_route.unsafe_format"},
		{"no_hvcc", "error.video_route.unsafe_format"},
		{"hlg_later", "error.video_route.unsafe_format"},
		{"passthrough_off", "error.video_route.passthrough_off"},
		{"not_hevc", "error.video_route.uhd_other_codec"},
	} {
		if got := ClassifyError(refused(http.StatusUnsupportedMediaType, over1080, c.reason, false)); got != c.want {
			t.Errorf("%s: %s, want %s", c.reason, got, c.want)
		}
		if _, ok := en[c.want]; !ok {
			t.Errorf("%s is not in locales/en.json", c.want)
		}
	}
	for _, key := range []string{"error.video_route.source_check_failed", "error.video_route.fallback_uhd"} {
		if _, ok := en[key]; !ok {
			t.Errorf("%s is not in locales/en.json", key)
		}
	}
}

// What production shows today does not move: a transcoder that names no
// reason, a start without a declaration (no_declaration -- every start
// until a browser opts in), a reason this build does not know, and a
// refusal whose body is not the over-1080p one all read by the old text.
func TestClassifyError_RouteRefusalLeavesTheOldTexts(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		body   string
		reason string
		want   string
	}{
		{"old transcoder, over 1080p", 415, over1080, "", "error.resolution_not_supported"},
		{"no declaration, over 1080p", 415, over1080, "no_declaration", "error.resolution_not_supported"},
		{"a reason not known here", 415, over1080, "some_future_reason", "error.resolution_not_supported"},
		// DISABLE_VIDEO_TRANSCODING: a 1080p HEVC is refused because
		// nothing is encoded here; "this is a 4K video" would be false.
		{"encoding disabled, declared", 415, "video transcoding is disabled\n", "needs_main10", "error.transcode_failed"},
		{"encoding disabled, capability off", 415, "video transcoding is disabled\n", "passthrough_off", "error.transcode_failed"},
		{"thp's 503 without a reason", 503, "", "", "error.transcode_unavailable"},
		{"a 503 naming another reason", 503, "source check failed\n", "needs_pq", "error.transcode_unavailable"},
	} {
		got := ClassifyError(refused(c.status, c.body, c.reason, false))
		if got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
		// The same text untyped -- what this error was before -- reads the
		// same key.
		untyped := errors.Wrap(errors.Errorf("transcoder session creation failed status=%d body=%s", c.status, c.body), "failed to buffer session HLS")
		if old := ClassifyError(untyped); old != got {
			t.Errorf("%s: typed %s, untyped %s", c.name, got, old)
		}
	}
}

// A source check that did not answer is not a source that cannot play:
// "try again", as a 503 the edge and clients read as retry-able.
func TestClassifyError_SourceCheckFailed(t *testing.T) {
	got := ClassifyError(refused(http.StatusServiceUnavailable, "source check failed\n", "probe_failed", false))
	if got != "error.video_route.source_check_failed" {
		t.Errorf("got %s", got)
	}
	if StatusForErrKey(got) != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", StatusForErrKey(got))
	}
	// A fallback restart whose check did not answer: still "try again".
	if got := ClassifyError(refused(http.StatusServiceUnavailable, "source check failed\n", "probe_failed", true)); got != "error.video_route.source_check_failed" {
		t.Errorf("fallback: %s", got)
	}
	for _, key := range []string{"error.video_route.needs_uhd_hevc", "error.video_route.fallback_uhd", "error.video_route.checking"} {
		if StatusForErrKey(key) != http.StatusInternalServerError {
			t.Errorf("%s: %d, want 500", key, StatusForErrKey(key))
		}
	}
}

// A restart after a passthrough failed in this browser declares nothing for
// the file, so the transcoder answers no_declaration: the fallback is read
// first, and the viewer is told this browser could not show it -- not that
// 4K is not streamed at all.
func TestClassifyError_FallbackRefusal(t *testing.T) {
	for _, reason := range []string{"no_declaration", "", "needs_pq"} {
		if got := ClassifyError(refused(http.StatusUnsupportedMediaType, over1080, reason, true)); got != "error.video_route.fallback_uhd" {
			t.Errorf("fallback, %q: %s", reason, got)
		}
	}
	// Not where nothing is encoded at all: the old text.
	if got := ClassifyError(refused(http.StatusUnsupportedMediaType, "video transcoding is disabled\n", "no_declaration", true)); got != "error.transcode_failed" {
		t.Errorf("fallback, encoding disabled: %s", got)
	}
}
