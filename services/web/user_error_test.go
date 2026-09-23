package web

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/common"
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
		// magnet resolution precedes the chain and has its own two faces
		"failed to magnetize: Post \"http://rest-api/resource/\": context deadline exceeded":           "error.magnet_no_metadata",
		"magnet timeout: rpc error: code = Canceled desc = context canceled":                           "error.magnet_no_metadata",
		"failed to magnetize: failed to parse magnet: error parsing v1 infohash \"urn:btih:5e4bd524\"": "error.magnet_invalid",
	}
	for msg, want := range cases {
		if got := ClassifyError(errors.New(msg)); got != want {
			t.Errorf("%q: got %s, want %s", msg, got, want)
		}
	}
	if StatusForErrKey("error.upstream_unavailable") != 503 {
		t.Error("upstream failures must be retry-able (503)")
	}
	if StatusForErrKey("error.magnet_no_metadata") != 504 || StatusForErrKey("error.magnet_invalid") != 400 {
		t.Error("magnet: no metadata is a gateway timeout, a broken link is a bad request")
	}
}

// From the form to the sentence: each input goes through the parser and the
// same two wrappers as handlers/resource.post (bindArgs, then post), and must
// land on its own key. Before 2026-09 all of these read "Invalid link or
// torrent file" — a broken magnet included, because "wrong resource provided"
// matched first — or, for text with a 5-hex run, a dead-magnet card.
func TestClassifyError_FormInput(t *testing.T) {
	const hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	v2 := "caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e"
	cases := []struct{ query, key string }{
		{"magnet:?xt=urn:btih:5e4bd524", "error.magnet_invalid"},
		{"magnet:?dn=only+a+name", "error.magnet_invalid"},
		{"Some Show S01E02", "error.free_text"},
		{"Some Movie 1999 1080p", "error.free_text"},
		{"https://torrents.example/torrent/12345/some-movie/", "error.webpage_url"},
		{"https://files.example/distro-2026.2-amd64.iso.torrent", "error.torrent_url"},
		{v2, "error.v2_hash"},
		{"magnet:?xt=urn:btmh:1220" + v2, "error.v2_hash"},
		// The query is part of the message; a title made of backend words
		// must not read as an outage, a ban or a login wall.
		{"Unavailable connection refused", "error.free_text"},
		{"PermissionDenied unauthorized", "error.free_text"},
		{"magnet:?xt=urn:btih:5e4bd524&dn=Service.Unavailable", "error.magnet_invalid"},
	}
	en := englishMessages(t)
	for _, tc := range cases {
		_, _, err := common.ResolveQueryHash(tc.query)
		if err == nil {
			t.Fatalf("%q: parsed, expected a refusal", tc.query)
		}
		err = errors.Wrap(errors.Wrapf(err, "wrong resource provided query=%v", tc.query), "wrong args provided")
		if got := ClassifyError(err); got != tc.key {
			t.Errorf("%q: got %s, want %s", tc.query, got, tc.key)
		}
		if _, ok := en[tc.key]; !ok {
			t.Errorf("%s is not in locales/en.json", tc.key)
		}
		if StatusForErrKey(tc.key) != 400 {
			t.Errorf("%s: status %d, want 400 — the input is wrong, not the server", tc.key, StatusForErrKey(tc.key))
		}
	}
	// A usable input is not an error at all.
	for _, q := range []string{hash, "magnet:?xt=urn:btih:" + hash, "https://torrents.example/" + hash} {
		if _, _, err := common.ResolveQueryHash(q); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
}

// The same broken magnet as text only — the shape it has when it did not come
// from our parser (a message relayed from rest-api, a log line replayed).
// "failed to parse magnet" must win over the "wrong resource provided"
// wrapper around it.
func TestClassifyError_BrokenMagnetBeatsItsWrapper(t *testing.T) {
	msg := `wrong args provided: wrong resource provided query=magnet:?xt=urn:btih:5e4bd524: failed to parse magnet: error parsing infohash "5e4bd524": unhandled xt parameter encoding (encoded length 8)`
	if got := ClassifyError(errors.New(msg)); got != "error.magnet_invalid" {
		t.Errorf("got %s, want error.magnet_invalid", got)
	}
	if got := ClassifyError(errors.New("wrong resource provided resource_id=favicon.png")); got != "error.invalid_resource" {
		t.Errorf("a bad resource id: got %s, want error.invalid_resource", got)
	}
}

func englishMessages(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../locales/en.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
