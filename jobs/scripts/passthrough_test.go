package scripts

import (
	"os"
	"testing"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

// The class a passthrough session's video needs is read from the master the
// transcoder wrote -- CODECS from the init it produced, RESOLUTION the
// source's -- the way content-transcoder admitted it (route.go
// routeForFacts): a class computed from the source's own probe would
// disagree where FFmpeg rebuilds the record (a Main10 profile over 8-bit
// pixels, a level that is not the head's), and a strike would then take
// the wrong tokens out of the declaration.
func TestPassthroughClass(t *testing.T) {
	for _, c := range []struct {
		name string
		v    hlsVariant
		want string
	}{
		{"Main10 at L4.0, 1080p", hlsVariant{Codecs: "hvc1.2.4.L120.90,mp4a.40.2", Width: 1920, Height: 1080}, "hevc10"},
		{"Main at L5.0, 1080p: the level asks for a 2160 decoder", hlsVariant{Codecs: "hvc1.1.6.L150.90,mp4a.40.2", Width: 1920, Height: 1080}, "hevc8-2160"},
		{"Main at L4.1, 1080p", hlsVariant{Codecs: "hvc1.1.6.L123.90", Width: 1920, Height: 800}, "hevc8"},
		{"Main10 4K", hlsVariant{Codecs: "hvc1.2.4.L153.90,mp4a.40.2", Width: 3840, Height: 2160}, "hevc10-2160"},
		{"Main10 at L4.1 but taller than 1080", hlsVariant{Codecs: "hvc1.2.4.L123.90", Width: 1920, Height: 1200}, "hevc10-2160"},
		{"wider than 1920 at L4.1", hlsVariant{Codecs: "hvc1.1.6.L123.90", Width: 2048, Height: 858}, "hevc8-2160"},
		{"tier High", hlsVariant{Codecs: "hvc1.2.4.H153.90", Width: 3840, Height: 2160}, "hevc10-2160"},
		{"hev1 entry", hlsVariant{Codecs: "hev1.1.6.L93.B0", Width: 1280, Height: 720}, "hevc8"},
		{"audio first", hlsVariant{Codecs: "mp4a.40.2,hvc1.2.4.L120.90", Width: 1920, Height: 1080}, "hevc10"},
		// Multichannel audio: CODECS lists every audio codec the session
		// puts out, Dolby copied as it is among them.
		{"Dolby and AAC beside the video", hlsVariant{Codecs: "hvc1.2.4.L153.90,mp4a.40.2,ec-3,ac-3", Width: 3840, Height: 2160}, "hevc10-2160"},
		{"Dolby first", hlsVariant{Codecs: "ec-3,hvc1.1.6.L120.90", Width: 1920, Height: 1080}, "hevc8"},
		{"no HEVC in CODECS", hlsVariant{Codecs: "avc1.42e00a,mp4a.40.2", Width: 1920, Height: 1080}, "unknown"},
		{"no CODECS", hlsVariant{Width: 3840, Height: 2160}, "unknown"},
		{"garbled level", hlsVariant{Codecs: "hvc1.2.4.Lxx.90"}, "unknown"},
	} {
		if got := passthroughClass(c.v); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// How long one passthrough segment may load: twice its time at the
// viewer's cap, never under hls.js's own 120 s, never over 15 min.
func TestPassthroughFragLoadMs(t *testing.T) {
	for _, c := range []struct {
		name      string
		bandwidth int64
		target    float64
		capBps    int64
		want      int
	}{
		{"no cap: the floor", 60_000_000, 10, 0, 120000},
		{"no bandwidth: the floor", 0, 10, 5_000_000, 120000},
		{"no target: the floor", 60_000_000, 0, 5_000_000, 120000},
		{"a light file under a cap: the floor", 3_000_000, 6, 5_000_000, 120000},
		{"4K remux at 5 Mbit/s: 2 x 30 Mbit/s x 10 s / 5 = 120 s", 30_000_000, 10, 5_000_000, 120000},
		{"4K remux at 5 Mbit/s: 2 x 40 x 10 / 5 = 160 s", 40_000_000, 10, 5_000_000, 160000},
		{"80 Mbit/s, 12 s segments, 2 Mbit/s: the ceiling", 80_000_000, 12, 2_000_000, 900000},
	} {
		if got := passthroughFragLoadMs(c.bandwidth, c.target, c.capBps); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// The status marks are made again only for a passthrough: every other
// route keeps what the steps before the session put on the stream.
func TestApplySessionRoute(t *testing.T) {
	s := &ActionScript{i18n: i18n.New(os.DirFS("../../locales"))}
	c := &web.Context{Lang: "ru", ApiClaims: &api.Claims{Rate: "5M"}}
	// A 1080p HEVC at 8 Mbit/s with one stereo AAC dub: re-encoded its rate
	// is not known; passed through it is 8 + the AAC (7.9 in the cap's megabit) -- over a 5M cap.
	const hevc = `{"format":{"bit_rate":"8400000"},"streams":[
		{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"tags":{"BPS":"8000000"}},
		{"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000","tags":{"BPS":"256000"}}]}`
	variant := hlsVariant{Codecs: "hvc1.2.4.L120.90,mp4a.40.2", Width: 1920, Height: 1080, Bandwidth: 9_000_000}

	for _, route := range []string{"reencode", "copy", "audio", ""} {
		sc := &StreamContent{MediaProbe: probeJSON(t, hevc)}
		s.setStatusMarks(sc, c, sc.MediaProbe, true)
		before := *sc
		s.applySessionRoute(sc, c, &SessionBufferResult{Session: &api.TranscoderSession{ID: "s", VideoRoute: route}, Variant: variant, TargetDuration: 10})
		if sc.StatusOverCap != before.StatusOverCap || sc.StatusFitsCap != before.StatusFitsCap || sc.StatusStallSub != before.StatusStallSub {
			t.Errorf("route %q: marks changed: %+v", route, sc)
		}
		if sc.VideoClass != "" || sc.FragLoadMs != 0 {
			t.Errorf("route %q: class %q, frag %d, want none", route, sc.VideoClass, sc.FragLoadMs)
		}
		if sc.TranscoderSession == nil || sc.TranscoderSession.VideoRoute != route {
			t.Errorf("route %q: session not kept", route)
		}
	}

	sc := &StreamContent{MediaProbe: probeJSON(t, hevc)}
	s.setStatusMarks(sc, c, sc.MediaProbe, true)
	if sc.StatusOverCap {
		t.Fatal("before the session a re-encoded HEVC's rate is unknown")
	}
	s.applySessionRoute(sc, c, &SessionBufferResult{Session: &api.TranscoderSession{ID: "s", VideoRoute: "passthrough", RouteReason: "ok"}, Variant: variant, TargetDuration: 10})
	if !sc.StatusOverCap || sc.StatusStallSub != "Без подписки — до 5 Мбит/с, а файлу нужно 7,9 Мбит/с" {
		t.Errorf("passthrough: over %v, sub %q", sc.StatusOverCap, sc.StatusStallSub)
	}
	if sc.VideoClass != "hevc10" {
		t.Errorf("passthrough class %q", sc.VideoClass)
	}
	// The cap as thp enforces "5M": 5·2^20 bits a second (capOf).
	if want := passthroughFragLoadMs(9_000_000, 10, 5<<20); sc.FragLoadMs != want {
		t.Errorf("frag %d, want %d", sc.FragLoadMs, want)
	}

	// No BANDWIDTH worth the name (the transcoder writes 1 without a
	// rate): the file's own rate stands in.
	sc = &StreamContent{MediaProbe: probeJSON(t, hevc)}
	s.applySessionRoute(sc, c, &SessionBufferResult{Session: &api.TranscoderSession{VideoRoute: "passthrough"}, Variant: hlsVariant{Codecs: "hvc1.1.6.L150.90", Bandwidth: 1}, TargetDuration: 60})
	if want := passthroughFragLoadMs(8_400_000, 60, 5<<20); sc.FragLoadMs != want || want == fragLoadFloorMs {
		t.Errorf("no bandwidth: frag %d, want %d (over the floor)", sc.FragLoadMs, want)
	}
}

// A passthrough is pulled at the source's own rate: a transcoded HEVC is
// "not known" only while it is re-encoded. Every existing caller passes
// videoCopied=false (playedBitrate) and keeps its answer (TestPlayedBitrate
// runs unchanged).
func TestPlayedBitrateRouted(t *testing.T) {
	const aac48 = 128 * 48000 / 44
	const hevc = `{"format":{"bit_rate":"5650625"},"streams":[{"codec_type":"video","codec_name":"hevc","tags":{"BPS":"4999862"}},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"}]}`
	if got, _ := playedBitrateRouted(probeJSON(t, hevc), true, false, nil); got != 0 {
		t.Errorf("re-encoded: %d, want 0", got)
	}
	if got := playedBitrate(probeJSON(t, hevc), true, nil); got != 0 {
		t.Errorf("playedBitrate: %d, want 0", got)
	}
	if got, _ := playedBitrateRouted(probeJSON(t, hevc), true, true, nil); got != int64(4999862+aac48) {
		t.Errorf("passed through: %d, want %d", got, 4999862+aac48)
	}
}
