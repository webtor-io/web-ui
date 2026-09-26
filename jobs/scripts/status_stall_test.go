package scripts

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

// The stream job knows what the status stream cannot: what the player pulls.
// Its line for the status's plan box says the cap in the viewer's terms and
// what the stream needs, in the cap's own unit.
func TestStatusStallSub(t *testing.T) {
	s := &ActionScript{i18n: i18n.New(os.DirFS("../../locales"))}
	ctx := func(rate, role, lang string) *web.Context {
		return &web.Context{Lang: lang, ApiClaims: &api.Claims{Rate: rate, Role: role}}
	}
	cases := []struct {
		name string
		c    *web.Context
		bps  int64
		want string
	}{
		// 8·1024² bits a second is "8" in the unit the cap is sold in.
		{"anonymous", ctx("5M", "", "ru"), 8 << 20, "Без подписки — до 5\u00a0Мбит/с, а файлу нужно 8\u00a0Мбит/с"},
		{"free, English", ctx("5M", "free", "en"), 8 << 20, "Without a subscription — up to 5\u00a0Mbps, and this file needs 8\u00a0Mbps"},
		{"paid", ctx("20M", "bronze", "ru"), 30 << 20, "Ваша подписка — до 20\u00a0Мбит/с, а файлу нужно 30\u00a0Мбит/с"},
		{"bitrate unknown: the cap alone", ctx("5M", "", "ru"), 0, "Без подписки — до 5\u00a0Мбит/с"},
		{"no cap: nothing binds", ctx("", "gold", "ru"), 8 << 20, ""},
		{"no claims", &web.Context{Lang: "ru"}, 8 << 20, ""},
	}
	for _, c := range cases {
		if got := s.statusStallSub(c.c, c.bps); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// A stream under the cap: the cap alone -- never "up to 5, needs 3" --
	// and, with statusview.FitsMargin to spare, the player marked so the
	// status says nothing while it plays.
	under := ctx("5M", "", "ru")
	if got := s.statusStallSub(under, 3<<20); got != "Без подписки — до 5\u00a0Мбит/с" {
		t.Errorf("under the cap: %q", got)
	}
	if !statusFitsCap(under, 3<<20) || statusFitsCap(under, 8<<20) || statusFitsCap(under, 0) || statusFitsCap(ctx("", "gold", "ru"), 3<<20) {
		t.Error("statusFitsCap")
	}
	// Under the cap by less than the margin: not marked, and still the cap
	// alone -- "up to 5, and this file needs 4.6" explains nothing.
	knick := int64(5183646 - 2*384000 + 128*48000/44)
	if statusFitsCap(under, knick) || statusOverCap(under, knick) {
		t.Error("The Knick, 4.34 at 5: neither fits nor over")
	}
	if got := s.statusStallSub(under, knick); got != "Без подписки — до 5\u00a0Мбит/с" {
		t.Errorf("within the margin: %q", got)
	}
	// Over the cap: the bitrate known and above it -- the stream box comes
	// as soon as it is due, not at the first stall. Unknown bitrate, no cap,
	// no claims: not marked (the box waits for a real stall).
	for _, c := range []struct {
		name string
		c    *web.Context
		bps  int64
		want bool
	}{
		{"8 over 5", under, 8 << 20, true},
		{"3 under 5", under, 3 << 20, false},
		{"exactly the cap", under, 5 << 20, false},
		{"bitrate unknown", under, 0, false},
		{"no cap", ctx("", "gold", "ru"), 8 << 20, false},
		{"no claims", &web.Context{Lang: "ru"}, 8 << 20, false},
	} {
		if got := statusOverCap(c.c, c.bps); got != c.want {
			t.Errorf("statusOverCap, %s: %v", c.name, got)
		}
		if c.want && statusFitsCap(c.c, c.bps) {
			t.Errorf("%s: over the cap and fitting it", c.name)
		}
	}
}

// probeJSON is a probe as content-prober answers it: ffprobe's own JSON.
func probeJSON(t *testing.T, js string) *api.MediaProbe {
	t.Helper()
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(js), &mp); err != nil {
		t.Fatal(err)
	}
	return &mp
}

// Probes of real files (content-prober replies, 2026-09-25/26; subtitle and
// cover streams kept where they were).
const (
	// The owner's report: 1080p H.264 with two stereo dubs, transcoder HLS.
	// The file reads 8.9 Mbps; the player pulls the video and one dub
	// re-encoded to AAC: 8.7.
	probeOwner = `{"format":{"bit_rate":"9352494"},"streams":[
		{"codec_type":"video","codec_name":"h264","height":1040,"tags":{"BPS":"8934213"}},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"192000","channels":2,"sample_rate":"48000","tags":{"BPS":"192000"}},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"224000","channels":2,"sample_rate":"48000","tags":{"BPS":"224000"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"BPS":"52"}}]}`
	// Seven dubs and a cover: 6.3 Mbps as a file, 4.8 as the stream.
	probeSevenDubs = `{"format":{"bit_rate":"6655734"},"streams":[
		{"codec_type":"video","codec_name":"h264","tags":{"BPS":"4855684"}},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"192000","channels":2,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"192000","channels":2,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"384000","channels":2,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"384000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"192000","channels":2,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"192000","channels":2,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"256000","channels":6,"sample_rate":"48000"},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"BPS":"93"}},
		{"codec_type":"video","codec_name":"mjpeg"}]}`
	// No statistics tags: the video is the file less its five dubs.
	probeDerived = `{"format":{"bit_rate":"8260633"},"streams":[
		{"codec_type":"video","codec_name":"h264"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"768000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"eac3","bit_rate":"768000","channels":6,"sample_rate":"48000"},
		{"codec_type":"subtitle","codec_name":"subrip"}]}`
	// A 0.7 Mbps re-encode whose tags still describe its 7.8 Mbps source.
	probeStale = `{"format":{"bit_rate":"718649"},"streams":[
		{"codec_type":"video","codec_name":"h264","tags":{"BPS":"7816333"}},
		{"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000","tags":{"BPS":"640000"}}]}`
	// The Knick s02e01 (9f99a3f2…): 720p H.264 without statistics tags,
	// two AC3 5.1 dubs. Estimated 4.56 Mbps (4.34 in the cap's megabit);
	// recorded at 5M, 2026-09-26, it pulled 5.29 (5.04) -- over the cap -- and
	// stalled four times in 180 s while marked "fits".
	probeKnick = `{"format":{"bit_rate":"5183646"},"streams":[
		{"codec_type":"video","codec_name":"h264","height":720},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"384000","channels":6,"sample_rate":"48000"},
		{"codec_type":"audio","codec_name":"ac3","bit_rate":"384000","channels":6,"sample_rate":"48000"}]}`
	// Sintel (08ada5a7…), an mp4 nginx-vod repackages: 1.2 Mbps; played at
	// 5M without a stall (2026-09-26).
	probeSintel = `{"format":{"bit_rate":"1164256"},"streams":[
		{"codec_type":"video","codec_name":"h264","bit_rate":"718146"},
		{"codec_type":"audio","codec_name":"aac","bit_rate":"440754","channels":6,"sample_rate":"48000"}]}`
)

// What the player pulls, not what the file weighs: one video track and one
// audio track, as the transcoder (or nginx-vod) serves them.
func TestPlayedBitrate(t *testing.T) {
	const aac48 = 128 * 48000 / 44 // libfdk_aac's default for a stereo pair at 48 kHz
	cases := []struct {
		name       string
		probe      string
		transcoded bool
		want       int64
	}{
		{"the owner's file: the video and one dub, re-encoded", probeOwner, true, 8934213 + aac48},
		{"seven dubs: one of them, and not the cover", probeSevenDubs, true, 4855684 + aac48},
		{"no tags: the file less its audio", probeDerived, true, 8260633 - 3*640000 - 2*768000 + aac48},
		{"stale tags: not known", probeStale, true, 0},
		{"The Knick: the file less its two dubs, one re-encoded", probeKnick, true, 5183646 - 2*384000 + aac48},
		{"Sintel through nginx-vod: its tracks as they are", probeSintel, false, 718146 + 440754},
		{"re-encoded video: the encoder's choice, not known",
			`{"format":{"bit_rate":"5650625"},"streams":[{"codec_type":"video","codec_name":"hevc","tags":{"BPS":"4999862"}},
			{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"}]}`, true, 0},
		{"no number for the video, one dub without one: not known",
			`{"format":{"bit_rate":"6000000"},"streams":[{"codec_type":"video","codec_name":"h264"},
			{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"},
			{"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000"}]}`, true, 0},
		{"stereo AAC is copied as it is",
			`{"format":{"bit_rate":"4300000"},"streams":[{"codec_type":"video","codec_name":"h264","tags":{"BPS":"4000000"}},
			{"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000","tags":{"BPS":"256000"}}]}`, true, 4256000},
		{"5.1 AAC is re-encoded to stereo",
			`{"format":{"bit_rate":"4500000"},"streams":[{"codec_type":"video","codec_name":"h264","tags":{"BPS":"4000000"}},
			{"codec_type":"audio","codec_name":"aac","channels":6,"sample_rate":"48000","tags":{"BPS":"384000"}}]}`, true, 4000000 + aac48},
		{"an mp4 through nginx-vod: the first tracks as they are, whatever the codec",
			`{"format":{"bit_rate":"7400000"},"streams":[{"codec_type":"video","codec_name":"hevc","bit_rate":"6000000"},
			{"codec_type":"audio","codec_name":"ac3","bit_rate":"640000","channels":6,"sample_rate":"48000"},
			{"codec_type":"audio","codec_name":"ac3","bit_rate":"640000","channels":6,"sample_rate":"48000"}]}`, false, 6640000},
		{"cover art before the film: the film's video, not the picture",
			`{"format":{"bit_rate":"4400000"},"streams":[{"codec_type":"video","codec_name":"mjpeg"},
			{"codec_type":"video","codec_name":"h264","tags":{"BPS":"4000000"}},
			{"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000","tags":{"BPS":"192000"}}]}`, true, 4192000},
		{"audio served as it is, its cover art first: the file",
			`{"format":{"bit_rate":"330000"},"streams":[{"codec_type":"video","codec_name":"mjpeg"},{"codec_type":"audio","codec_name":"mp3","bit_rate":"320000"}]}`, false, 330000},
		{"audio through the transcoder: stereo AAC",
			`{"format":{"bit_rate":"950000"},"streams":[{"codec_type":"audio","codec_name":"flac","channels":2,"sample_rate":"44100"}]}`, true, 128 * 44100 / 44},
		{"no streams: not known", `{"format":{"bit_rate":"8000000"}}`, true, 0},
	}
	for _, c := range cases {
		if got := playedBitrate(probeJSON(t, c.probe), c.transcoded); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	if playedBitrate(nil, true) != 0 {
		t.Error("no probe")
	}
}

// What the stream job puts on its StreamContent, for the player element, at
// a 5 Mbps cap: marked by the stream, so a file whose dubs made it heavy
// but whose stream is under the cap is not sold a faster plan while it
// plays (it was, by the file's own rate). "Fits" -- nothing said while it
// plays -- only with statusview.FitsMargin to spare: a stream estimated
// within it of the cap is unknown (the fact while it plays, the box at a
// real stall), and its line never reads "needs 4.6" next to "up to 5".
func TestSetStatusMarks(t *testing.T) {
	s := &ActionScript{i18n: i18n.New(os.DirFS("../../locales"))}
	under := &web.Context{Lang: "ru", ApiClaims: &api.Claims{Rate: "5M"}}
	for _, c := range []struct {
		name       string
		probe      string
		transcoded bool
		fits, over bool
		sub        string
	}{
		{"over: the owner's file", probeOwner, true, false, true, "Без подписки — до 5\u00a0Мбит/с, а файлу нужно 8,7\u00a0Мбит/с"},
		{"fits with room: Sintel, 1.1", probeSintel, false, true, false, "Без подписки — до 5\u00a0Мбит/с"},
		{"within the margin: The Knick, 4.34", probeKnick, true, false, false, "Без подписки — до 5\u00a0Мбит/с"},
		{"heavy with dubs, the stream within the margin: 4.76", probeSevenDubs, true, false, false, "Без подписки — до 5\u00a0Мбит/с"},
		{"no tags, the stream within the margin: 4.72", probeDerived, true, false, false, "Без подписки — до 5\u00a0Мбит/с"},
		{"stale tags: unknown", probeStale, true, false, false, "Без подписки — до 5\u00a0Мбит/с"},
		{"unknown", `{}`, true, false, false, "Без подписки — до 5\u00a0Мбит/с"},
	} {
		sc := &StreamContent{}
		s.setStatusMarks(sc, under, probeJSON(t, c.probe), c.transcoded)
		if sc.StatusFitsCap != c.fits || sc.StatusOverCap != c.over || sc.StatusStallSub != c.sub {
			t.Errorf("%s: fits %v, over %v, sub %q", c.name, sc.StatusFitsCap, sc.StatusOverCap, sc.StatusStallSub)
		}
	}
}
