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
	// Internal transcoder failures keep the generic message.
	err := errors.Errorf("transcoder session creation failed status=500 body=%s", "failed to start transcoding\n")
	if got := ClassifyError(err); got != "error.generic" {
		t.Errorf("got %q, want error.generic", got)
	}
}
