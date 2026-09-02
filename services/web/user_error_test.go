package web

import (
	"testing"

	"github.com/pkg/errors"
)

func TestClassifyError_ResolutionNotSupported(t *testing.T) {
	// Exact shape produced by the real chain: content-transcoder returns
	// 415 with its reason as body, api.CreateTranscoderSession embeds it,
	// hls.go/action.go wrap it on the way up.
	err := errors.Wrap(
		errors.Wrap(
			errors.Errorf("transcoder session creation failed status=415 body=%s", "resolution over 1080p is not supported\n"),
			"failed to create transcoder session"),
		"failed to buffer session HLS")

	if got := ClassifyError(err); got != "error.resolution_not_supported" {
		t.Errorf("got %q, want error.resolution_not_supported", got)
	}
}

func TestClassifyError_GenericTranscoderFailure(t *testing.T) {
	// Internal transcoder failures are the converter's, not the file's:
	// "couldn't start, try again or download" — never the codec wording.
	err := errors.Errorf("transcoder session creation failed status=500 body=%s", "failed to start transcoding\n")
	if got := ClassifyError(err); got != "error.transcode_unavailable" {
		t.Errorf("got %q, want error.transcode_unavailable", got)
	}
}

// Each streaming-chain failure class gets its own key with an action the user
// can take; an unknown message still falls back to error.generic (the negative
// control that keeps the classifier honest).
func TestClassifyError_StreamingChain(t *testing.T) {
	cases := map[string]string{
		"failed to buffer session HLS: failed to create transcoder session: transcoder session creation failed status=415 body=unsupported codec hevc": "error.transcode_failed",
		"failed to buffer session HLS: session buffer timeout exceeded: context deadline exceeded":                                                     "error.stream_stalled",
		"failed to fetch session video playlist: Get \"http://x\": EOF":                                                                                "error.stream_stalled",
		"no video variant found in master playlist":                                                                                                    "error.stream_stalled",
		"transcoder session creation failed status=503 body=too many failed auto-restarts":                                                             "error.transcode_unavailable",
		"failed to get probe data: content prober returned 500":                                                                                        "error.probe_failed",
		"failed to retrieve stream url: export failed":                                                                                                 "error.upstream_unavailable",
		"failed to retrieve download link: timeout":                                                                                                    "error.upstream_unavailable",
		"stats returned status 429":                                                                                                                    "error.upstream_unavailable",
		// still first: the more specific transcoder refusal
		"failed to create transcoder session: transcoder session creation failed status=415 body=resolution over 1080p is not supported": "error.resolution_not_supported",
		// and the older, more specific wrappers keep winning over the chain
		"failed to retrieve resource: access is forbidden url=x": "error.forbidden",
		"failed to retrieve resource: resource not found":        "error.not_found",
		"something nobody anticipated":                           "error.generic",
	}
	for msg, want := range cases {
		if got := ClassifyError(errors.New(msg)); got != want {
			t.Errorf("%q: got %s, want %s", msg, got, want)
		}
	}
	if StatusForErrKey("error.upstream_unavailable") != 503 {
		t.Error("upstream failures must be retry-able (503)")
	}
}
