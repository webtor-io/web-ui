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

// `types` is rest-api's parameter and must keep its semantics: CSV, trimmed,
// unknown value rejected, absent meaning "all". The valid set comes from
// ra.ExportTypes, so this also fails if upstream ever renames a type.
func TestParseExportTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		param  string
		want   []string
		reject bool
	}{
		{name: "absent means all", param: "", want: nil},
		{name: "single", param: "stream", want: []string{"stream"}},
		{name: "csv with spaces", param: " download , stream ", want: []string{"download", "stream"}},
		{name: "every documented type", param: "download,stream,torrent_client_stat,subtitles,media_probe",
			want: []string{"download", "stream", "torrent_client_stat", "subtitles", "media_probe"}},
		{name: "unknown type rejected", param: "stream,hls", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExportTypes(tc.param)
			if tc.reject {
				if err == nil || err.Code != libapi.CodeBadRequest {
					t.Fatalf("want bad_request, got err=%v types=%v", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want nil (not given), got %v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Fatalf("missing %q in %v", w, got)
				}
			}
		})
	}
}
