package scripts

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

// rest-api sends a file to nginx-vod or to the transcoder by its extension
// alone (services/content_info.go shouldTranscode): an .mp4 goes to
// nginx-vod, which hands the browser the source's own codecs, video and
// audio muxed in one segment. Where the browser cannot take the audio, it
// plays nothing, or plays without sound -- and not with an error the viewer
// sees: hls.js cannot add the SourceBuffer
// (addSourceBuffer('video/mp4;codecs=ec-3,hev1...') throws
// NotSupportedError, a fatal bufferAddCodecError) and the old route's
// handling recovers it again every ~110 ms for as long as the page is open
// (2026-09-30: a 2160p HEVC MP4 with E-AC-3 in Chrome 154; 7 player-dead
// why=recovering on vod in 12 h). Such an MP4 goes to the transcoder
// instead, which encodes the audio as the declaration allows.
//
// What nginx-vod serves (nginx-vod-module 26f06877, the one our image
// builds):
//   - the first video and the first audio track it counts (v1-a1). It
//     counts, for HLS, H.264, HEVC and AV1 video and AAC, MP3, AC-3, E-AC-3,
//     FLAC and DTS stored as mp4a audio (mp4_parser.c; ngx_http_vod_hls.c
//     SUPPORTED_CODECS); a track in any other codec -- VP9, VP8, Opus, ALAC,
//     PCM, TrueHD ("mlpa"), DTS in dtsc/dtsh boxes -- is skipped and takes
//     no index, so a1 is the next audio track. Whatever the viewer picks:
//     the pick is the job's key and the list's default, not the stream's.
//   - fMP4 when its first track is a video that is not H.264, MPEG-TS
//     otherwise (ngx_http_vod_hls_get_container_format, container "auto").
//     In fMP4 the browser's MSE decodes the audio: AC-3 and E-AC-3 play
//     where it does. In MPEG-TS hls.js demuxes, and nginx-vod writes E-AC-3
//     under AC-3's stream type (0x81, mpegts_encoder_filter.c): a browser
//     without AC-3 drops it (silence), one with AC-3 has E-AC-3 frames fed
//     to the AC-3 parser -- E-AC-3 in TS plays nowhere.
//
// A browser plays AC-3 or E-AC-3 only where its declaration says so (ac3,
// ec3). A declaration without them -- none at all, "unknown", video tokens
// only, a restart after a failed passthrough (the page sends none), the
// page's 7-day memory of one -- counts as a browser that cannot: most
// cannot (Chrome, Firefox), and a start without the token is most often a
// file that already failed here. What that costs: a browser that could
// (Safari on its first page, before its audio answer) gets AAC from the
// transcoder instead of Dolby from nginx-vod, and a 2160p HEVC the
// transcoder cannot pass through (HLG, Dolby Vision 5) its refusal.

// Why an MP4 is sent to the transcoder instead of nginx-vod
// (webui_vod_reroute_total{reason}).
const (
	vodRerouteEAC3TS   = "eac3_ts"        // E-AC-3 in nginx-vod's MPEG-TS: no hls.js plays it
	vodRerouteEAC3     = "eac3"           // E-AC-3, and the declaration has no ec3
	vodRerouteAC3      = "ac3"            // AC-3, and the declaration has no ac3
	vodRerouteNoDecode = "no_decoder"     // DTS: no browser decodes it
	vodRerouteUnserved = "unserved_audio" // audio nginx-vod serves none of: silence on nginx-vod
)

// vodVideoCodecs are the video codecs nginx-vod's HLS serves (ffprobe names).
var vodVideoCodecs = map[string]bool{"h264": true, "hevc": true, "av1": true}

// vodAudioCodecs are the audio codecs nginx-vod's HLS serves (ffprobe
// names); an MP4's audio track in any other is skipped. DTS counts only
// stored as mp4a, which ffprobe does not tell apart from dtsc: counted, so
// such a file goes to the transcoder (it plays there) where nginx-vod may
// have served the next track.
var vodAudioCodecs = map[string]bool{"aac": true, "mp3": true, "ac3": true, "eac3": true, "flac": true, "dts": true}

// vodReroute is why the MP4 probed as mp, which rest-api sends to
// nginx-vod, must go to the transcoder for the browser that declared decl,
// or "" where nginx-vod plays it.
func vodReroute(mp *api.MediaProbe, decl models.DecodeRequest) string {
	if mp == nil {
		return ""
	}
	// MPEG-TS unless the first video nginx-vod serves is not H.264.
	ts := true
	for _, st := range mp.Streams {
		if st.CodecType == "video" && vodVideoCodecs[st.CodecName] {
			ts = st.CodecName == "h264"
			break
		}
	}
	audio := false
	for _, st := range mp.Streams {
		if st.CodecType != "audio" {
			continue
		}
		audio = true
		if !vodAudioCodecs[st.CodecName] {
			continue
		}
		// The first audio track nginx-vod serves (a1) decides.
		switch c := st.CodecName; {
		case c == "dts":
			return vodRerouteNoDecode
		case c == "eac3" && ts:
			return vodRerouteEAC3TS
		case c == "eac3" && !decl.Declares("ec3"):
			return vodRerouteEAC3
		case c == "ac3" && !decl.Declares("ac3"):
			return vodRerouteAC3
		}
		return ""
	}
	if audio {
		return vodRerouteUnserved
	}
	return ""
}

// transcodeURLFromVOD is the transcoder's stream URL of the file whose
// nginx-vod stream URL is vodURL: rest-api's two URLs differ only after the
// file (services/url_builder.go BuildVODURL, BuildTranscodeURL):
// "<file>~vod/hls/<id>/index.m3u8?<q>" and "<file>~hls/index.m3u8?<q>".
// The string is cut, not re-encoded: the file's path keeps its escaping
// byte for byte.
func transcodeURLFromVOD(vodURL string) (string, error) {
	path, query := vodURL, ""
	if i := strings.IndexByte(vodURL, '?'); i >= 0 {
		path, query = vodURL[:i], vodURL[i:]
	}
	i := strings.Index(path, "~vod/")
	if i < 0 {
		return "", errors.Errorf("not an nginx-vod stream URL")
	}
	return path[:i] + "~hls/index.m3u8" + query, nil
}
