package api

import (
	"net/http"
	"testing"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/libapi"
)

// The rest-api client discards the upstream status and leaves only the message,
// so these substrings are the whole signal. They are the same ones rest-api's
// error middleware matches to pick that status — if it ever stops using them,
// this test is where that shows up, rather than in a client seeing 502 for a
// torrent that simply does not exist.
func TestUpstreamErrorMapsToUpstreamStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    string
		status int
		code   string
	}{
		{"not found", "resource not found", http.StatusNotFound, libapi.CodeNotFound},
		{"forbidden", "access is forbidden url=…", http.StatusForbidden, libapi.CodeForbidden},
		{"parse", "failed to parse limit", http.StatusBadRequest, libapi.CodeBadRequest},
		{"timeout", "magnet fetch timeout", http.StatusRequestTimeout, libapi.CodeUpstreamTimeout},
		{"deadline", "context deadline exceeded", http.StatusGatewayTimeout, libapi.CodeUpstreamTimeout},
		{"anything else", "connection reset by peer", http.StatusBadGateway, libapi.CodeUpstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := upstreamError(errors.New(tc.err), "failed")
			if got.Status != tc.status || got.Code != tc.code {
				t.Errorf("%q -> %d/%s, want %d/%s", tc.err, got.Status, got.Code, tc.status, tc.code)
			}
			// The upstream message stays in the logs, never in the body: it
			// carries internal URLs and query strings.
			if got.Message == tc.err {
				t.Errorf("upstream message leaked into the response: %q", got.Message)
			}
		})
	}
}

// An infohash addressed in a different case must be the same torrent in the
// store, in the library and in the Vault — otherwise a delete issued in the
// other case silently misses.
func TestNormalizeResourceID(t *testing.T) {
	const want = "08ada5a7a6183aae1e09d831df6748d566095a10"
	for _, in := range []string{
		"08ADA5A7A6183AAE1E09D831DF6748D566095A10",
		"  08ada5a7a6183aae1e09d831df6748d566095a10 ",
		"08Ada5a7A6183aae1e09d831df6748d566095a10",
	} {
		if got := normalizeResourceID(in); got != want {
			t.Errorf("normalizeResourceID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPageLimit(t *testing.T) {
	if got := pageLimit(0); got != defaultPageLimit {
		t.Errorf("pageLimit(0) = %d, want the default %d", got, defaultPageLimit)
	}
	if got := pageLimit(maxPageLimit * 10); got != maxPageLimit {
		t.Errorf("pageLimit(huge) = %d, want it capped at %d", got, maxPageLimit)
	}
	if got := pageLimit(7); got != 7 {
		t.Errorf("pageLimit(7) = %d, want 7", got)
	}
}
