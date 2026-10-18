package scripts

import (
	"reflect"
	"strings"
	"testing"
)

// buildFileInfo is the whole of what the card claims about a file. The rules
// a hard-of-hearing viewer decides on -- which audio starts, which subtitles
// are inside, which of them are SDH or signs only -- each have a case here.
//
// Negative control (run 2026-10-18): with the "first flagged wins" guard
// (`audioDefault < 0 &&`) removed, "every track flagged" goes red -- the last
// track wins; with the no-flag fallback removed, "no flag" goes red; without
// the SDH title fallback the subtitles case, without the attached_pic skip
// the cover-art case. Without the denied-word guard (titleSays) or the "."
// in sdhTitleRe's boundary, the denied-word case goes red; without the
// closed_captions read, the captions case; without the bitrate estimate,
// both approximate cases, and without its one-video guard, the two-video one.
func TestBuildFileInfo(t *testing.T) {
	type tr = FileInfoTrack
	cases := []struct {
		name  string
		probe string
		want  FileInfo
	}{
		{
			name: "every track flagged default: only the first is the default",
			probe: `{"format":{"duration":"6723.4"},"streams":[
				{"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,"color_transfer":"smpte2084","tags":{"BPS":"8800000"},"disposition":{"default":1}},
				{"codec_type":"audio","codec_name":"eac3","channel_layout":"5.1(side)","tags":{"language":"rus","title":"Dub"},"disposition":{"default":1}},
				{"codec_type":"audio","codec_name":"ac3","channel_layout":"stereo","tags":{"language":"eng"},"disposition":{"default":1}}]}`,
			want: FileInfo{
				Duration: "1:52:03",
				Video:    []FileInfoVideo{{Codec: "HEVC", Width: 3840, Height: 2160, HDR: "HDR10", Mbps: 8800000.0 / (1 << 20)}},
				Audio: []tr{
					{Lang: "ru", Title: "Dub", Codec: "EAC3", Channels: "5.1(side)", Default: true},
					{Lang: "en", Codec: "AC3", Channels: "stereo"},
				},
			},
		},
		{
			name: "the flag on the second track moves the default there",
			probe: `{"streams":[
				{"codec_type":"audio","codec_name":"aac","tags":{"language":"ger"},"disposition":{"default":0}},
				{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"},"disposition":{"default":1}}]}`,
			want: FileInfo{Audio: []tr{{Lang: "de", Codec: "AAC"}, {Lang: "en", Codec: "AAC", Default: true}}},
		},
		{
			name: "no flag: the first audio plays, no subtitle is default",
			probe: `{"streams":[
				{"codec_type":"audio","codec_name":"aac"},
				{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng"}}]}`,
			want: FileInfo{
				Audio:     []tr{{Codec: "AAC", Default: true}, {Lang: "en", Codec: "AAC"}},
				Subtitles: []tr{{Lang: "en", Codec: "SRT"}},
			},
		},
		{
			name: "subtitles: picture-based kept and marked, SDH and forced by flag or by title",
			probe: `{"streams":[
				{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng"},"disposition":{"default":1}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English SDH"}},
				{"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng"},"disposition":{"hearing_impaired":1,"default":1}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"eng_forced"}},
				{"codec_type":"subtitle","codec_name":"dvd_subtitle","tags":{"language":"fre"},"disposition":{"forced":1}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"title":"Unforced Accessible"}},
				{"codec_type":"subtitle","codec_name":"hdmv_text_subtitle","tags":{"language":"und"}}]}`,
			want: FileInfo{Subtitles: []tr{
				{Lang: "en", Codec: "PGS", Picture: true, Default: true},
				{Lang: "en", Title: "English SDH", Codec: "SRT", SDH: true},
				{Lang: "en", Codec: "ASS", SDH: true},
				{Lang: "en", Title: "eng_forced", Codec: "SRT", Forced: true},
				{Lang: "fr", Codec: "VobSub", Picture: true, Forced: true},
				{Title: "Unforced Accessible", Codec: "SRT"},
				{Codec: "HDMV_TEXT_SUBTITLE"},
			}},
		},
		{
			name: "a denied word is no badge; a .cc domain is not CC",
			probe: `{"streams":[
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Non-SDH)"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"No SDH"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English SDH (no forced)"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Non-Forced"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Subs by site.cc"}},
				{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English [CC]"}}]}`,
			want: FileInfo{Subtitles: []tr{
				{Lang: "en", Title: "English (Non-SDH)", Codec: "SRT"},
				{Lang: "en", Title: "No SDH", Codec: "SRT"},
				{Lang: "en", Title: "English SDH (no forced)", Codec: "SRT", SDH: true},
				{Lang: "en", Title: "Non-Forced", Codec: "SRT"},
				{Lang: "en", Title: "Subs by site.cc", Codec: "SRT"},
				{Lang: "en", Title: "English [CC]", Codec: "SRT", SDH: true},
			}},
		},
		{
			name: "captions inside the video stream (CEA-608/708) are said, with no subtitle stream",
			probe: `{"streams":[
				{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"closed_captions":1},
				{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]}`,
			want: FileInfo{
				Video:          []FileInfoVideo{{Codec: "H264", Width: 1920, Height: 1080}},
				Audio:          []tr{{Lang: "en", Codec: "AAC", Default: true}},
				ClosedCaptions: true,
			},
		},
		{
			name: "cover art is not a video stream; HLG; no stream bitrate: the container's, approximate",
			probe: `{"format":{"duration":"59.6","bit_rate":"9000000"},"streams":[
				{"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,"disposition":{"attached_pic":1}},
				{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"color_transfer":"arib-std-b67"},
				{"codec_type":"audio","codec_name":"opus","channel_layout":"stereo","tags":{"language":"jpn"}},
				{"codec_type":"data","codec_name":"bin_data"}]}`,
			want: FileInfo{
				Duration: "0:01:00",
				Video:    []FileInfoVideo{{Codec: "HEVC", Width: 1920, Height: 1080, HDR: "HLG", Mbps: 9000000.0 / (1 << 20), Approx: true}},
				Audio:    []tr{{Lang: "ja", Codec: "OPUS", Channels: "stereo", Default: true}},
			},
		},
		{
			name: "no video bitrate: the container's less what the audio records",
			probe: `{"format":{"bit_rate":"4000000"},"streams":[
				{"codec_type":"video","codec_name":"h264","width":1280,"height":720},
				{"codec_type":"audio","codec_name":"ac3","bit_rate":"448000","tags":{"language":"rus"},"disposition":{"default":1}},
				{"codec_type":"audio","codec_name":"ac3","bit_rate":"448000","tags":{"language":"eng"}}]}`,
			want: FileInfo{
				Video: []FileInfoVideo{{Codec: "H264", Width: 1280, Height: 720, Mbps: 3104000.0 / (1 << 20), Approx: true}},
				Audio: []tr{{Lang: "ru", Codec: "AC3", Default: true}, {Lang: "en", Codec: "AC3"}},
			},
		},
		{
			name: "two videos without bitrates: no estimate (the file does not say how they share it)",
			probe: `{"format":{"bit_rate":"4000000"},"streams":[
				{"codec_type":"video","codec_name":"h264","width":1280,"height":720},
				{"codec_type":"video","codec_name":"h264","width":640,"height":360}]}`,
			want: FileInfo{Video: []FileInfoVideo{{Codec: "H264", Width: 1280, Height: 720}, {Codec: "H264", Width: 640, Height: 360}}},
		},
		{
			name:  "nothing inside: empty lists, no duration",
			probe: `{"format":{"duration":"N/A"},"streams":[]}`,
			want:  FileInfo{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildFileInfo(probeJSON(t, c.probe))
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("got  %+v\nwant %+v", *got, c.want)
			}
		})
	}
}

func TestFileInfoBitrateLabel(t *testing.T) {
	v := FileInfoVideo{Mbps: 8.44}
	if got := v.BitrateLabel("en"); got != "8.4" {
		t.Errorf("en: %q", got)
	}
	if got := v.BitrateLabel("ru"); got != "8,4" {
		t.Errorf("ru: %q", got)
	}
	if got := (FileInfoVideo{}).BitrateLabel("en"); got != "" {
		t.Errorf("no bitrate: %q", got)
	}
}

// The raw view is ffprobe's JSON minus the one field that is ours, not the
// file's: format.filename, the ~cp source URL with its credentials.
// Negative control (run 2026-10-18): without the delete, the filename case
// goes red; without RedactURL, the tag case.
func TestRawProbeJSON(t *testing.T) {
	got := rawProbeJSON([]byte(`{"streams":[{"index":0,"codec_name":"h264","tags":{"comment":"see https://x.example/?token=abc123"}}],
		"format":{"filename":"https://thp.example/h/f.mkv~cp?api-key=k1&token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl","format_name":"matroska,webm","bit_rate":"4000000"}}`))
	for _, w := range []string{`"codec_name": "h264"`, `"format_name": "matroska,webm"`, `"bit_rate": "4000000"`, "\n  \"streams\": ["} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	for _, leak := range []string{"filename", "thp.example", "k1", "eyJhbGciOiJIUzI1NiJ9", "abc123"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaks %q:\n%s", leak, got)
		}
	}
	if got := rawProbeJSON([]byte(`{"format":"https://thp.example/?token=x","streams":[]}`)); strings.Contains(got, "thp.example") {
		t.Errorf("a format that is not an object stays: %s", got)
	}
	if got := rawProbeJSON([]byte(`not json`)); got != "" {
		t.Errorf("not JSON: %q", got)
	}
	big := rawProbeJSON([]byte(`{"streams":[{"tags":{"title":"` + strings.Repeat("x", rawProbeLimit) + `"}}]}`))
	if len(big) > rawProbeLimit+len("\n…") || !strings.HasSuffix(big, "…") {
		t.Errorf("not capped: %d bytes", len(big))
	}
}
