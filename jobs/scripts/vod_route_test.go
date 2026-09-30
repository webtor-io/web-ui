package scripts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// probeOf is a media probe with streams "type:codec", in order.
func probeOf(t *testing.T, streams ...string) *api.MediaProbe {
	t.Helper()
	var ss []map[string]string
	for _, s := range streams {
		typ, codec, _ := strings.Cut(s, ":")
		ss = append(ss, map[string]string{"codec_type": typ, "codec_name": codec})
	}
	b, _ := json.Marshal(map[string]any{"streams": ss})
	var mp api.MediaProbe
	if err := json.Unmarshal(b, &mp); err != nil {
		t.Fatal(err)
	}
	return &mp
}

const (
	chromeDecl = "hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq,aac51" // the 2026-09-30 repro
	dolbyDecl  = "hevc8,hevc10,aac51,ac3,ec3"
)

func TestVODReroute(t *testing.T) {
	type start struct{ decode, fallback, class string }
	chrome, dolby, none := start{chromeDecl, "", ""}, start{dolbyDecl, "", ""}, start{}
	for _, c := range []struct {
		name    string
		streams []string
		start   start
		want    string
	}{
		// The repro: HEVC (fMP4) with E-AC-3, a browser without ec3.
		{"HEVC + E-AC-3, no ec3", []string{"video:hevc", "audio:eac3", "audio:eac3", "video:mjpeg"}, chrome, vodRerouteEAC3},
		{"HEVC + E-AC-3, ec3 declared", []string{"video:hevc", "audio:eac3"}, dolby, ""},
		// Without the token, whatever the reason, a browser that cannot.
		{"HEVC + E-AC-3, nothing declared", []string{"video:hevc", "audio:eac3"}, none, vodRerouteEAC3},
		{"HEVC + E-AC-3, unknown", []string{"video:hevc", "audio:eac3"}, start{models.DecodeUnknown, "", ""}, vodRerouteEAC3},
		{"HEVC + E-AC-3, video tokens only", []string{"video:hevc", "audio:eac3"}, start{"hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq", "", ""}, vodRerouteEAC3},
		// A restart after a failed passthrough sends no declaration: it must
		// not land back on nginx-vod's dead player.
		{"restart after a passthrough failure", []string{"video:hevc", "audio:eac3"}, start{"", "decode_error", "hevc10"}, vodRerouteEAC3},
		{"restart in compatibility mode", []string{"video:hevc", "audio:eac3"}, start{"", "user", "hevc10-2160"}, vodRerouteEAC3},
		{"restart after an aac51 failure", []string{"video:h264", "audio:ac3"}, start{"", "media_error", "aac51"}, vodRerouteAC3},
		// H.264 is MPEG-TS: E-AC-3 plays in no hls.js, whatever is declared.
		{"H.264 + E-AC-3, ec3 declared", []string{"video:h264", "audio:eac3"}, dolby, vodRerouteEAC3TS},
		{"audio only + E-AC-3 (MPEG-TS)", []string{"audio:eac3"}, dolby, vodRerouteEAC3TS},
		// Video nginx-vod does not serve is skipped: the audio comes first, MPEG-TS.
		{"VP9 + E-AC-3, ec3 declared", []string{"video:vp9", "audio:eac3"}, dolby, vodRerouteEAC3TS},
		{"cover first, then HEVC + E-AC-3 declared", []string{"video:mjpeg", "video:hevc", "audio:eac3"}, dolby, ""},
		{"cover first, then H.264 + E-AC-3", []string{"video:mjpeg", "video:h264", "audio:eac3"}, dolby, vodRerouteEAC3TS},
		// AC-3 needs its own token, in either container.
		{"H.264 + AC-3, no ac3", []string{"video:h264", "audio:ac3"}, chrome, vodRerouteAC3},
		{"HEVC + AC-3, only ec3", []string{"video:hevc", "audio:ac3"}, start{"hevc8,ec3", "", ""}, vodRerouteAC3},
		{"H.264 + AC-3, ac3 declared", []string{"video:h264", "audio:ac3"}, dolby, ""},
		// DTS nginx-vod serves and no browser decodes.
		{"DTS", []string{"video:h264", "audio:dts"}, dolby, vodRerouteNoDecode},
		// a1 is the first audio track nginx-vod serves, not ffprobe's first.
		{"AAC first, E-AC-3 second, no ec3", []string{"video:hevc", "audio:aac", "audio:eac3"}, chrome, ""},
		{"E-AC-3 first, AAC second, no ec3", []string{"video:hevc", "audio:eac3", "audio:aac"}, chrome, vodRerouteEAC3},
		{"Opus first (skipped), then E-AC-3", []string{"video:hevc", "audio:opus", "audio:eac3"}, chrome, vodRerouteEAC3},
		{"TrueHD first (skipped), then AAC", []string{"video:hevc", "audio:truehd", "audio:aac"}, none, ""},
		{"PCM first (skipped), then AC-3", []string{"video:h264", "audio:pcm_s16le", "audio:ac3"}, chrome, vodRerouteAC3},
		// Audio nginx-vod serves none of: silence there.
		{"Opus only", []string{"video:h264", "audio:opus"}, dolby, vodRerouteUnserved},
		{"TrueHD only", []string{"video:hevc", "audio:truehd"}, none, vodRerouteUnserved},
		// What plays stays.
		{"H.264 + AAC", []string{"video:h264", "audio:aac"}, chrome, ""},
		{"HEVC + AAC, nothing declared", []string{"video:hevc", "audio:aac"}, none, ""},
		{"H.264 + MP3", []string{"video:h264", "audio:mp3"}, none, ""},
		{"no audio at all", []string{"video:h264", "subtitle:mov_text"}, none, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			decl := models.ParseDecodeRequest(c.start.decode, c.start.fallback, c.start.class)
			if got := vodReroute(probeOf(t, c.streams...), decl); got != c.want {
				t.Errorf("vodReroute(%v, %+v) = %q, want %q", c.streams, decl, got, c.want)
			}
		})
	}
	if got := vodReroute(nil, models.ParseDecodeRequest(chromeDecl, "", "")); got != "" {
		t.Errorf("no probe: %q, want \"\"", got)
	}
}

func TestTranscodeURLFromVOD(t *testing.T) {
	// The repro's URL, escaping and all: the path must come out byte for byte.
	file := "https://abra--x.api.test/a8894150faa50678d9055e691bded4eb64a33b62/Film.2026.2160p.DV.HDR10+.DDP5.1.H265.MP4/Film.2026.2160p.DV.HDR10+%5BBen%20The%20Men%5D.mp4"
	q := "?api-key=K&token=T"
	got, err := transcodeURLFromVOD(file + "~vod/hls/54e2e4589dae9c8f230b0cabd55e211ddb9fc9f1/index.m3u8" + q)
	if err != nil {
		t.Fatal(err)
	}
	if want := file + "~hls/index.m3u8" + q; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// And the transcoder's session base is what it has always been made of.
	if base, err := sessionBaseURL(got); err != nil || base != file+"~hls"+q {
		t.Errorf("session base %q, %v", base, err)
	}
	if got, err := transcodeURLFromVOD(file + "~vod/hls/x/index.m3u8"); err != nil || got != file+"~hls/index.m3u8" {
		t.Errorf("no query: %q, %v", got, err)
	}
	for _, bad := range []string{
		file + "~hls/index.m3u8" + q,
		file + "/index.m3u8?next=~vod/hls/x",
		"",
	} {
		if got, err := transcodeURLFromVOD(bad); err == nil {
			t.Errorf("%q: %q, want an error", bad, got)
		}
	}
}

// The rerouted MP4 opens a transcoder session like any MKV: the session is
// asked for at the file's ~hls, with the start's declaration.
func TestRerouteOpensATranscoderSession(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + "?" + r.URL.RawQuery
		http.Error(w, "stop here", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	u, err := transcodeURLFromVOD(srv.URL + "/abc/f.mp4~vod/hls/54e2/index.m3u8?token=T")
	if err != nil {
		t.Fatal(err)
	}
	s := &ActionScript{api: directAPI(t), c: &web.Context{Lang: "en"}, i18n: i18n.New(os.DirFS("../../locales")), warmup: WarmupSettings{TimeoutMin: 1}}
	j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
	_, _ = s.bufferSessionHLS(context.Background(), j, u, time.Second, models.ParseDecodeRequest(chromeDecl, "", ""))
	if !strings.HasPrefix(asked, "/abc/f.mp4~hls/session?") || !strings.Contains(asked, "decode=") {
		t.Errorf("session asked at %q, want /abc/f.mp4~hls/session with the declaration", asked)
	}
}
