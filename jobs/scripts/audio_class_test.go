package scripts

import (
	"context"
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

// What a declaration made of a session's audio, read from its master
// (docs/player.md, "Multichannel audio and the fallback"). The master of a
// start that declared nothing -- today's, byte for byte -- says neither
// CHANNELS nor a Dolby codec, so it reads "".
func TestSessionAudioClass(t *testing.T) {
	const (
		stream  = "#EXT-X-STREAM-INF:BANDWIDTH=5000000,CODECS=\"avc1.42e00a,mp4a.40.2\",AUDIO=\"audio\"\nv0-720.m3u8\n"
		streamD = "#EXT-X-STREAM-INF:BANDWIDTH=42002567,RESOLUTION=3840x1606,CODECS=\"hvc1.2.4.L153.90,%s\",AUDIO=\"a\"\nv0.m3u8\n"
	)
	media := func(attrs string) string {
		return "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",LANGUAGE=\"eng\",NAME=\"English\"" + attrs + ",URI=\"a0.m3u8\"\n"
	}
	for _, c := range []struct {
		name, master, want string
	}{
		{"the old route's master, as it has always been", "#EXTM3U\n" + media("") + media("") + stream, ""},
		{"stereo, said so", "#EXTM3U\n" + media(`,CHANNELS="2"`) + stream, ""},
		{"mono", "#EXTM3U\n" + media(`,CHANNELS="1"`) + stream, ""},
		{"5.1 AAC", "#EXTM3U\n" + media(`,CHANNELS="2"`) + media(`,CHANNELS="6"`) + stream, "aac51"},
		{"Atmos's count without a Dolby codec named", "#EXTM3U\n" + media(`,CHANNELS="16/JOC"`) + stream, "aac51"},
		{"E-AC-3", "#EXTM3U\n" + media(`,CHANNELS="16/JOC"`) + strings.Replace(streamD, "%s", "ec-3", 1), "dolby"},
		{"AC-3 over a 5.1 AAC rendition", "#EXTM3U\n" + media(`,CHANNELS="6"`) + strings.Replace(streamD, "%s", "ac-3", 1), "dolby"},
		{"the mp4a spelling", "#EXTM3U\n" + media("") + strings.Replace(streamD, "%s", "mp4a.a6", 1), "dolby"},
		{"a later variant", "#EXTM3U\n" + stream + strings.Replace(streamD, "%s", "EC-3", 1), "dolby"},
		{"a subtitle rendition's CHANNELS is not audio", "#EXTM3U\n#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"s\",NAME=\"x\",CHANNELS=\"6\",URI=\"s.m3u8\"\n" + stream, ""},
		{"garbage", "#EXTM3U\n" + media(`,CHANNELS="six"`) + stream, ""},
		{"empty", "", ""},
	} {
		if got := sessionAudioClass(c.master); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The session's audio class comes back from buffering only for a start that
// declared an audio token: a start that declared none cannot have had its
// audio changed, whatever the master says, and its player must stay what it
// was (no data-audio-class, no restart of its own).
func TestBufferSessionHLSReadsAudioClassOnlyForAnAudioDeclaration(t *testing.T) {
	const master = "#EXTM3U\n" +
		"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",LANGUAGE=\"eng\",NAME=\"English\",CHANNELS=\"6\",URI=\"a0.m3u8\"\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=5000000,CODECS=\"avc1.42e00a,mp4a.40.2\",AUDIO=\"audio\"\nv0.m3u8\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/session"):
			_, _ = w.Write([]byte(`{"id":"s1","duration":10,"video_route":"reencode","route_reason":"no_declaration"}`))
		case strings.HasSuffix(r.URL.Path, "/session/s1/index.m3u8"):
			_, _ = w.Write([]byte(master))
		case strings.HasSuffix(r.URL.Path, "/session/s1/v0.m3u8"):
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10.0,\nv0-0.ts\n#EXT-X-ENDLIST\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := &ActionScript{api: directAPI(t), c: &web.Context{Lang: "en"}, i18n: i18n.New(os.DirFS("../../locales")), warmup: WarmupSettings{TimeoutMin: 1}}
	for _, c := range []struct {
		decode, want string
	}{
		{"", ""},
		{"hevc8,hevc10,hdr-pq", ""},
		{"aac51", "aac51"},
		{"hevc8,aac51,ec3", "aac51"},
	} {
		j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
		res, err := s.bufferSessionHLS(context.Background(), j, srv.URL+"/abc/f.mkv~hls/index.m3u8", time.Second, models.DecodeRequest{Decode: c.decode})
		if err != nil {
			t.Fatalf("%q: %v", c.decode, err)
		}
		if res.AudioClass != c.want {
			t.Errorf("decode %q: audio class %q, want %q", c.decode, res.AudioClass, c.want)
		}
	}
}

// The player restarts a file by itself (and needs its item id) on a
// passthrough, and on a start that declared an audio token; never on a start
// that declared nothing, nor on one that declared video only on the old
// route.
func TestStreamContentPlayerRestarts(t *testing.T) {
	sc := func(route, decode string) *StreamContent {
		c := &StreamContent{VideoStreamUserData: &models.VideoStreamUserData{}}
		c.VideoStreamUserData.Decode = decode
		if route != "" {
			c.TranscoderSession = &api.TranscoderSession{VideoRoute: route}
		}
		return c
	}
	for _, c := range []struct {
		route, decode string
		want          bool
	}{
		{"", "", false},
		{"reencode", "", false},
		{"reencode", "hevc8,hdr-pq", false},
		{"reencode", "aac51", true},
		{"copy", "hevc8,ac3", true},
		{"passthrough", "hevc8", true},
	} {
		if got := sc(c.route, c.decode).PlayerRestarts(); got != c.want {
			t.Errorf("%q %q: %v", c.route, c.decode, got)
		}
	}
	if (&StreamContent{}).PlayerRestarts() {
		t.Error("no user data: no restart")
	}
}
