package scripts

import (
	"context"
	"flag"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pkg/errors"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

func TestParseMasterVideoVariantURL(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="rus",NAME="Russian",URI="a0.m3u8?api-key=abc&token=xyz"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="eng",NAME="English",URI="a1.m3u8?api-key=abc&token=xyz"
#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=5000000,CODECS="avc1.42e00a,mp4a.40.2",AUDIO="audio"
v0-720.m3u8?api-key=abc&token=xyz`

	got, err := parseMasterVideoVariantURL(master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "v0-720.m3u8?api-key=abc&token=xyz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseMasterVideoVariantURL_NoVariant(t *testing.T) {
	body := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="eng",NAME="English",URI="a0.m3u8"
`
	_, err := parseMasterVideoVariantURL(body)
	if err == nil {
		t.Fatal("expected error for missing variant")
	}
}

func TestParseMediaPlaylist_WithSegments(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-TARGETDURATION:11
#EXTINF:10.051000,
v0-720-0.ts?api-key=abc&token=xyz
#EXTINF:8.758000,
v0-720-1.ts?api-key=abc&token=xyz
#EXTINF:9.500000,
v0-720-2.ts?api-key=abc&token=xyz
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endList {
		t.Error("expected endList=false")
	}
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0].URL != "v0-720-0.ts?api-key=abc&token=xyz" {
		t.Errorf("segment 0 URL = %q", segments[0].URL)
	}
	if math.Abs(segments[0].Duration-10.051) > 0.001 {
		t.Errorf("segment 0 duration = %f, want 10.051", segments[0].Duration)
	}
	if math.Abs(segments[1].Duration-8.758) > 0.001 {
		t.Errorf("segment 1 duration = %f, want 8.758", segments[1].Duration)
	}
}

func TestParseMediaPlaylist_WithEndList(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-TARGETDURATION:10
#EXTINF:10.000000,
seg0.ts
#EXTINF:5.000000,
seg1.ts
#EXT-X-ENDLIST
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !endList {
		t.Error("expected endList=true")
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
}

func TestParseMediaPlaylist_Empty(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-TARGETDURATION:10
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endList {
		t.Error("expected endList=false")
	}
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments, got %d", len(segments))
	}
}

func TestResolveURL_Relative(t *testing.T) {
	base := "https://example.com/stream/master.m3u8?api-key=abc&token=xyz"
	target := "v0-720.m3u8?api-key=abc&token=xyz"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com/stream/v0-720.m3u8?api-key=abc&token=xyz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveURL_Absolute(t *testing.T) {
	base := "https://example.com/stream/master.m3u8"
	target := "https://cdn.example.com/v0-720.m3u8?token=abc"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestResolveURL_RootRelative(t *testing.T) {
	base := "https://example.com/stream/master.m3u8"
	target := "/other/path.m3u8"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com/other/path.m3u8"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// directAPI is an api.Api that sends Download straight to the URL it is
// given. The flag is set explicitly: flags read their env vars on Apply, and
// a shell with USE_INTERNAL_TORRENT_HTTP_PROXY set would rewrite the host.
func directAPI(t *testing.T) *api.Api {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--use-internal-torrent-http-proxy=false"}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), http.DefaultClient)
}

// One poll of the session video playlist, per answer. Only the restart cap
// ends the buffering; the transient answers must keep it polling, because
// the next poll is what restarts FFmpeg (504) or reaches the transcoder
// again (thp's empty 503, a misrouted 404). The bodies are written the way
// the services write them: http.Error, text plus a newline.
func TestPollSessionPlaylist(t *testing.T) {
	const playlist = "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.000000,\nv0-0.ts\n#EXTINF:4.000000,\nv0-1.ts\n"
	cases := []struct {
		name     string
		status   int
		body     string
		terminal bool
		segments int
	}{
		{name: "restart cap: 503 with its text", status: http.StatusServiceUnavailable, body: "transcoder restart limit reached", terminal: true},
		{name: "thp stub: 503, empty body", status: http.StatusServiceUnavailable, body: ""},
		{name: "FFmpeg died: 504 playlist timeout", status: http.StatusGatewayTimeout, body: "playlist timeout"},
		{name: "misrouted: 404 session not found", status: http.StatusNotFound, body: "session not found"},
		{name: "200 with segments", status: http.StatusOK, body: playlist, segments: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusOK {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				if tc.body == "" {
					w.WriteHeader(tc.status)
					return
				}
				http.Error(w, tc.body, tc.status)
			}))
			defer srv.Close()

			segments, endList, err := pollSessionPlaylist(context.Background(), directAPI(t), srv.URL+"/session/s1/v0.m3u8")
			if !tc.terminal {
				if err != nil {
					t.Fatalf("err=%v, want nil: this answer must keep the buffer polling", err)
				}
				if len(segments) != tc.segments || endList {
					t.Fatalf("segments=%d endList=%v, want %d and false", len(segments), endList, tc.segments)
				}
				return
			}
			if err == nil {
				t.Fatal("err=nil: the restart cap must end the buffering, not poll to the deadline")
			}
			// The shape action.go hands the job: its wrapper around ours.
			wrapped := errors.Wrap(err, "failed to buffer session HLS")
			if got := web.ClassifyError(wrapped); got != "error.transcode_failed" {
				t.Fatalf("ClassifyError(%q)=%s, want error.transcode_failed", wrapped, got)
			}
			// ErrorWrapperScript turns a deadline into the no-peers modal;
			// this must not look like one.
			if errors.Is(errors.Cause(wrapped), context.DeadlineExceeded) {
				t.Fatalf("%v reads as the buffer deadline", wrapped)
			}
		})
	}
}

// A passthrough master, as content-transcoder writes it: the variant's
// attributes are read (CODECS holds commas inside its quotes), and the old
// route's master reads as it always did.
func TestParseMasterVideoVariant(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="eng",NAME="English",URI="a0.m3u8?token=xyz"
#EXT-X-STREAM-INF:BANDWIDTH=41234567,RESOLUTION=3840x1606,CODECS="hvc1.2.4.L153.90,mp4a.40.2",VIDEO-RANGE=PQ,AUDIO="audio"
v0.m3u8?token=xyz`
	v, err := parseMasterVideoVariant(master)
	if err != nil {
		t.Fatal(err)
	}
	want := hlsVariant{URL: "v0.m3u8?token=xyz", Codecs: "hvc1.2.4.L153.90,mp4a.40.2", Width: 3840, Height: 1606, Bandwidth: 41234567}
	if v != want {
		t.Errorf("got %+v, want %+v", v, want)
	}
	old, err := parseMasterVideoVariant(`#EXTM3U
#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=5000000,CODECS="avc1.42e00a,mp4a.40.2",AUDIO="audio"
v0-720.m3u8`)
	if err != nil || old != (hlsVariant{URL: "v0-720.m3u8", Codecs: "avc1.42e00a,mp4a.40.2", Bandwidth: 5000000}) {
		t.Errorf("old route master: %+v, %v", old, err)
	}
}

// The target duration is read with the segments; a playlist without one
// reads 0, and the segments it lists are the same as parseMediaPlaylist's.
func TestParseMediaPlaylistTarget(t *testing.T) {
	body := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:11\n#EXT-X-MAP:URI=\"init-0.mp4\"\n#EXTINF:10.427000,\nv0-0.m4s\n#EXTINF:9.009000,\nv0-1.m4s\n"
	segs, end, target, err := parseMediaPlaylistTarget(body)
	if err != nil || end || target != 11 || len(segs) != 2 {
		t.Fatalf("segs %d end %v target %v err %v", len(segs), end, target, err)
	}
	old, oldEnd, oldErr := parseMediaPlaylist(body)
	if oldErr != nil || oldEnd != end || len(old) != len(segs) || old[0] != segs[0] || old[1] != segs[1] {
		t.Errorf("parseMediaPlaylist disagrees: %+v", old)
	}
	if _, _, target, _ := parseMediaPlaylistTarget("#EXTM3U\n#EXTINF:4.0,\nv0-0.ts\n"); target != 0 {
		t.Errorf("no target: %v", target)
	}
	// The longest segment seen: a keyframe-cut passthrough can run over
	// the announced target, and an EVENT playlist's target can grow.
	if got := maxSegmentDuration(8, 11, []hlsSegment{{Duration: 10.4}, {Duration: 12.5}}); got != 12.5 {
		t.Errorf("maxSegmentDuration %v", got)
	}
	if got := maxSegmentDuration(14, 11, []hlsSegment{{Duration: 10.4}}); got != 14 {
		t.Errorf("maxSegmentDuration keeps the previous %v", got)
	}
}
