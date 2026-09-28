package scripts

import (
	"strconv"
	"strings"

	"github.com/webtor-io/web-ui/services/web"
)

// videoRoutePassthrough is content-transcoder's name for a session that
// hands the browser the source's video as it is (HEVC passthrough).
const videoRoutePassthrough = "passthrough"

// HEVC levels (level_idc = 30 × level): 4.1, the ceiling of the 1080
// tokens (content-transcoder route.go hevcLevel41).
const hevcLevel41 = 123

// passthroughClass is the decoder class a passthrough session's video
// needs -- the token of the declaration it was admitted on, and the class a
// failure in the browser is charged to. It is read from the master's own
// description of the stream, the way content-transcoder decided it
// (route.go routeForFacts): CODECS carries the OUTPUT's profile and level
// (the transcoder writes it from the init it produced; the source's hvcC
// can differ, FFmpeg rebuilds the record), RESOLUTION the source's size.
//
//	10-bit: profile 2 (Main10)
//	2160:   taller than 1080 or wider than 1920, or level over 4.1
//
// "unknown" when the master names no HEVC codec it can read.
func passthroughClass(v hlsVariant) string {
	profile, level, ok := hevcProfileLevel(v.Codecs)
	if !ok {
		return "unknown"
	}
	cls := "hevc8"
	if profile == 2 {
		cls = "hevc10"
	}
	if v.Height > 1080 || v.Width > 1920 || level > hevcLevel41 {
		cls += "-2160"
	}
	return cls
}

// hevcProfileLevel reads general_profile_idc and general_level_idc from the
// first HEVC entry of a CODECS list (ISO/IEC 14496-15 Annex E:
// hvc1.[A-C]<profile>.<compat>.<L|H><level>[.<constraints>]).
func hevcProfileLevel(codecs string) (profile, level int, ok bool) {
	for _, c := range strings.Split(codecs, ",") {
		parts := strings.Split(strings.TrimSpace(c), ".")
		if len(parts) < 4 || (parts[0] != "hvc1" && parts[0] != "hev1") {
			continue
		}
		p := strings.TrimLeft(parts[1], "ABC")
		pv, err := strconv.Atoi(p)
		if err != nil || len(parts[3]) < 2 || (parts[3][0] != 'L' && parts[3][0] != 'H') {
			return 0, 0, false
		}
		lv, err := strconv.Atoi(parts[3][1:])
		if err != nil {
			return 0, 0, false
		}
		return pv, lv, true
	}
	return 0, 0, false
}

// Bounds of the time one passthrough segment may take to load
// (passthroughFragLoadMs).
const (
	fragLoadFloorMs   = 120000
	fragLoadCeilingMs = 900000
)

// passthroughFragLoadMs is how long the player lets one passthrough segment
// load before it counts as a timeout: twice the time the segment takes at
// the viewer's cap, between 2 and 15 minutes. hls.js otherwise gives every
// segment 120 s and then fetches it again from zero (fragLoadPolicy): a
// 4K remux segment of 10 s at 60 Mbit/s is 75 MB, 120 s at a 5 Mbit/s cap
// -- it would never finish and be fetched over and over (plan P-I2).
//
// bandwidth is the variant's BANDWIDTH (bits/s: the larger of the source's
// average and its first segment's rate, content-transcoder), target the
// longest segment in seconds, capBps the viewer's cap in bits/s (0: none,
// and the floor applies).
func passthroughFragLoadMs(bandwidth int64, target float64, capBps int64) int {
	if capBps <= 0 || bandwidth <= 0 || target <= 0 {
		return fragLoadFloorMs
	}
	ms := 2 * float64(bandwidth) * target / float64(capBps) * 1000
	switch {
	case ms < fragLoadFloorMs:
		return fragLoadFloorMs
	case ms > fragLoadCeilingMs:
		return fragLoadCeilingMs
	}
	return int(ms)
}

// capOf is the viewer's cap in bits/s from the claims, 0 for none.
func capOf(c *web.Context) int64 {
	if c == nil || c.ApiClaims == nil {
		return 0
	}
	return parseRateLimit(c.ApiClaims.Rate)
}

// applySessionRoute puts the transcoder session on sc, and with it what its
// route decides. For a passthrough: the status marks again (the ones made
// before the session were for a re-encode of HEVC, whose rate is unknown;
// the source's video as it is has its own, so the cap card and the "this
// file needs N" work as for H.264), the decoder class a failure is charged
// to, and the segment load time. Every other route leaves sc as the steps
// before made it.
func (s *ActionScript) applySessionRoute(sc *StreamContent, c *web.Context, result *SessionBufferResult) {
	sc.TranscoderSession = result.Session
	if !sc.Passthrough() {
		return
	}
	if sc.MediaProbe != nil {
		s.setRoutedStatusMarks(sc, c, sc.MediaProbe, true, true)
	}
	sc.VideoClass = passthroughClass(result.Variant)
	bw := result.Variant.Bandwidth
	if bw <= 1 && sc.MediaProbe != nil {
		// No BANDWIDTH worth the name (content-transcoder writes 1 when it
		// knows no rate): the file's own rate.
		bw = getVideoBitrate(sc.MediaProbe)
	}
	sc.FragLoadMs = passthroughFragLoadMs(bw, result.TargetDuration, capOf(c))
}
