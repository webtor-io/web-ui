package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// probeOf is a media probe with streams "type:codec" or
// "type:codec:WxH", in order.
func probeOf(t *testing.T, streams ...string) *api.MediaProbe {
	t.Helper()
	var ss []map[string]any
	for _, s := range streams {
		parts := strings.Split(s, ":")
		st := map[string]any{"codec_type": parts[0], "codec_name": parts[1]}
		if len(parts) > 2 {
			var w, h int
			if _, err := fmt.Sscanf(parts[2], "%dx%d", &w, &h); err != nil {
				t.Fatal(err)
			}
			st["width"], st["height"] = w, h
		}
		ss = append(ss, st)
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
		{"H.264 + AC-3, nothing declared", []string{"video:h264", "audio:ac3"}, none, vodRerouteAC3},
		{"HEVC + E-AC-3, video tokens only", []string{"video:hevc", "audio:eac3"}, start{"hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq", "", ""}, vodRerouteEAC3},
		// A restart after the browser failed the file -- whichever reason --
		// goes to the transcoder: never back on nginx-vod's dead player.
		{"restart after a passthrough failure", []string{"video:hevc", "audio:eac3"}, start{"", "decode_error", "hevc10"}, vodRerouteFallback},
		{"restart in compatibility mode", []string{"video:hevc", "audio:eac3"}, start{"", "user", "hevc10-2160"}, vodRerouteFallback},
		{"restart after an aac51 failure", []string{"video:h264", "audio:ac3"}, start{"", "media_error", "aac51"}, vodRerouteFallback},
		{"restart after nginx-vod's stream was refused", []string{"video:h264", "audio:aac"}, start{chromeDecl, "vod_codecs", "vod"}, vodRerouteFallback},
		// HEVC is held to a declaration that answered: hevc8 at all,
		// hevc8-2160 over 1080. One that did not answer keeps nginx-vod (the
		// player gives it up if the browser refuses it).
		{"HEVC 2160 + AAC, nothing declared", []string{"video:hevc:3840x2160", "audio:aac"}, none, ""},
		{"HEVC 2160 + AAC, unknown", []string{"video:hevc:3840x2160", "audio:aac"}, start{models.DecodeUnknown, "", ""}, ""},
		{"HEVC + AAC, audio tokens only (answered: no HEVC)", []string{"video:hevc", "audio:aac"}, start{"aac51,ac3,ec3", "", ""}, vodRerouteHEVC},
		{"HEVC 1080 + AAC, hevc8 only", []string{"video:hevc:1920x1080", "audio:aac"}, start{"hevc8", "", ""}, ""},
		{"HEVC 2160 + AAC, hevc8 only", []string{"video:hevc:3840x2160", "audio:aac"}, start{"hevc8,hevc10", "", ""}, vodRerouteHEVC},
		{"HEVC 1920x800 wide scope + AAC, hevc8 only", []string{"video:hevc:1920x800", "audio:aac"}, start{"hevc8", "", ""}, ""},
		{"HEVC 2048x858 + AAC, hevc8 only", []string{"video:hevc:2048x858", "audio:aac"}, start{"hevc8", "", ""}, vodRerouteHEVC},
		{"HEVC 2160 + AAC, 2160 declared", []string{"video:hevc:3840x2160", "audio:aac"}, chrome, ""},
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
		{"TrueHD first (skipped), then AAC", []string{"video:hevc", "audio:truehd", "audio:aac"}, chrome, ""},
		{"PCM first (skipped), then AC-3", []string{"video:h264", "audio:pcm_s16le", "audio:ac3"}, chrome, vodRerouteAC3},
		// Audio nginx-vod serves none of: silence there.
		{"Opus only", []string{"video:h264", "audio:opus"}, dolby, vodRerouteUnserved},
		{"TrueHD only", []string{"video:hevc", "audio:truehd"}, chrome, vodRerouteUnserved},
		// What plays stays.
		{"H.264 + AAC", []string{"video:h264", "audio:aac"}, chrome, ""},
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
	// A restart after the browser refused nginx-vod's stream goes to the
	// transcoder with no probe too: it must not land back on nginx-vod.
	if got := vodReroute(nil, models.ParseDecodeRequest(chromeDecl, "vod_codecs", "vod")); got != vodRerouteFallback {
		t.Errorf("a restart with no probe: %q, want %q", got, vodRerouteFallback)
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

// The player gives an nginx-vod stream up to a restart (vod-guard.js) and
// needs the file's item id for it: data-item-id is on every ~vod stream.
func TestPlayerRestartsOffVOD(t *testing.T) {
	tag := func(src string) *StreamContent {
		return &StreamContent{ExportTag: &ra.ExportTag{Sources: []ra.ExportSource{{Src: src}}}}
	}
	if !tag("https://x.test/h/a.mp4~vod/hls/54e2/index.m3u8?token=T").PlayerRestarts() {
		t.Error("an nginx-vod stream: no restart")
	}
	if tag("https://x.test/h/a.mp3").PlayerRestarts() {
		t.Error("a file served as it is: restarts")
	}
	if (&StreamContent{}).PlayerRestarts() {
		t.Error("no export tag: restarts")
	}
}
