package scripts

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/language"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

// playedBitrate is playedBitrateRouted before the route is known: a
// transcoded video that is not H.264 counts as re-encoded.
func playedBitrate(mp *api.MediaProbe, transcoded bool, vsud *models.VideoStreamUserData) int64 {
	bps, _ := playedBitrateRouted(mp, transcoded, false, vsud)
	return bps
}

// playedBitrateRouted is the bitrate, in bits a second, of what the viewer's player
// pulls for the probed file, or 0 when that is not known. The transfer
// status's marks (StatusFitsCap, StatusOverCap) and its "…and this file
// needs N Mbps" compare it with the viewer's cap; a file whose number is not
// known is never marked over (at most "fits", by its ceiling), and its
// stream box waits for a real stall.
// It is an estimate of the tracks' average; what crosses thp is MPEG-TS
// segments of a stretch of the film: the transcoder's AAC came to ~236
// kbit/s in TS against the 139.6 below, and a 720p file's first 132 s to
// 1.16 times its estimate in all (statusview.FitsMargin, which "fits"
// keeps clear of).
//
// It is not the file's own bitrate. The player pulls one video track and one
// audio track: the transcoder's HLS (content-transcoder, Online mode) copies
// H.264 video as it is, re-encodes any other video, and serves each audio
// track as a rendition of its own, as the declaration has it made
// (audioOut), of which the player fetches the one it starts on
// (startAudio); nginx-vod's HLS of an mp4 repackages its first video and
// audio tracks as they are. The file's
// rate (ffprobe's format bit_rate) counts every dub, commentary and lossless
// track besides: a 3.5 Mbps H.264 with two 1.5 Mbps DTS dubs reads 6.6 and
// would be sold a faster plan while it plays smoothly at 5 (24 h of transcoder
// probes, 2026-09-26: of 202 H.264 files over a 5 Mbps cap by the file's
// rate, 36 are under it by the stream's and 14 have no number to tell).
//
// ceiling is, where bps is 0, a rate the stream does not pull more than on
// average, or 0 for none: the file, for tracks served as they are (they
// are a part of it), plus the rate of an audio encode; the transcoder's
// cap on its re-encode of a video (encodedVideoCeiling) plus the audio. It
// says a stream fits under a cap (StatusFitsCap), never that it is over or
// what it needs: a bound, not an estimate.
//
// transcoded is the stream export's own word (ExportMeta.Transcode): the
// file goes through the transcoder, else it is served as it is (nginx-vod's
// HLS for a video, the file itself for audio). videoCopied is the
// transcoder session's: its route hands the browser the source's video as
// it is (passthrough), so even a video that is not H.264 is pulled at its
// own rate, and its audio is fMP4. It is known only once the session
// exists; before that, false (playedBitrate). vsud is the start's: its
// declaration and the viewer's track choice; nil for neither.
func playedBitrateRouted(mp *api.MediaProbe, transcoded bool, videoCopied bool, vsud *models.VideoStreamUserData) (bps, ceiling int64) {
	if mp == nil {
		return 0, 0
	}
	file := parseBitrate(mp.Format.BitRate)
	video, audio := -1, []int{}
	for i, s := range mp.Streams {
		switch s.CodecType {
		case "video":
			// Cover art is a video stream too; the transcoder skips it
			// the same way (NewHLS).
			if video < 0 && s.CodecName != "mjpeg" && s.CodecName != "png" {
				video = i
			}
		case "audio":
			audio = append(audio, i)
		}
	}
	own := func(i int) int64 { return streamBitrate(mp, i) }
	// The audio track the player pulls, as it gets it. No audio adds
	// nothing, and that is known.
	var a int64
	aCopied := true
	if len(audio) > 0 {
		k := 0
		if transcoded && video >= 0 {
			// nginx-vod and an audio file have only the first.
			k = startAudio(mp, vsud)
		}
		var d models.DecodeRequest
		if vsud != nil {
			d = vsud.DecodeRequest
		}
		a, aCopied = audioOut(mp, audio[k], transcoded, d, videoCopied)
	}
	// Statistics tags a later remux copied unchanged describe another
	// file: a 720p re-encode at 0.7 Mbps still said 7.8 for its video. The
	// streams of a file cannot add up to more than the file: of 767 files
	// whose video and audio all have a number, 741 add up to 0.9-1.02 of
	// it, 25 (stale) to 1.1 and more, one in between.
	stale := false
	if file > 0 {
		var sum int64
		if video >= 0 {
			sum += own(video)
		}
		for _, i := range audio {
			sum += own(i)
		}
		stale = sum*100 > file*105
	}
	reencoded := video >= 0 && transcoded && !videoCopied && mp.Streams[video].CodecName != "h264"
	switch {
	case video < 0:
	case reencoded:
		if v := encodedVideoCeiling(mp.Streams[video].Height); v > 0 && (len(audio) == 0 || a > 0 && !(stale && aCopied)) {
			ceiling = v + a
		}
	case file > 0 && aCopied:
		ceiling = file
	case file > 0 && a > 0:
		ceiling = file + a
	}
	if stale {
		return 0, ceiling
	}
	if video < 0 {
		if len(audio) == 0 {
			return 0, 0
		}
		if !transcoded {
			// The file itself.
			return file, 0
		}
		return a, 0
	}
	if reencoded {
		// Re-encoded at a quality target (CRF): the rate is the encoder's
		// choice, known only once it is made.
		return 0, ceiling
	}
	v := own(video)
	if v == 0 {
		// No number of its own: the file less its audio, when every audio
		// track has one. What is left besides the video -- subtitles,
		// the container's own bytes -- is a percent or two.
		if file == 0 {
			return 0, ceiling
		}
		v = file
		for _, i := range audio {
			r := own(i)
			if r == 0 {
				return 0, ceiling
			}
			v -= r
		}
		if v <= 0 {
			return 0, ceiling
		}
	}
	if len(audio) == 0 {
		return v, 0
	}
	if a == 0 {
		return 0, ceiling
	}
	return v + a, 0
}

// audioOut is the bitrate the player pulls for the audio stream i, 0 when
// not known, and whether that is the stream as it is (copied). The
// transcoder's rule (content-transcoder services/audio.go audioOutputFor,
// sha-22f64b9): AAC with at most two channels is copied; with the
// declaration's aac51, so is AAC of up to six channels in a layout ADTS
// can say (aacConfigLayouts), and with ec3 / ac3, on fMP4 (a passthrough's
// audio) only, E-AC-3 / AC-3 of more than two channels; any other track of
// more than two channels is encoded to AAC 5.1 at aac51BitRate under
// aac51; everything else to stereo AAC with libfdk_aac at its default
// rate, which FFmpeg sets as 128 kbit per channel pair times the sample
// rate over 44 kHz (libfdk-aacenc.c: 139.6 kbps at 48 kHz). Not
// transcoded: the stream as it is. The transcoder's own fallback -- a copy
// a muxer refused is encoded from the next start -- is not foreseen here.
func audioOut(mp *api.MediaProbe, i int, transcoded bool, d models.DecodeRequest, fmp4 bool) (int64, bool) {
	s := mp.Streams[i]
	if !transcoded {
		return streamBitrate(mp, i), true
	}
	ch, aac51 := s.Channels, d.Declares("aac51")
	switch {
	case s.CodecName == "aac" && ch <= 2,
		s.CodecName == "aac" && ch <= 6 && aac51 && aacConfigLayouts[s.ChannelLayout],
		s.CodecName == "eac3" && ch > 2 && d.Declares("ec3") && fmp4,
		s.CodecName == "ac3" && ch > 2 && d.Declares("ac3") && fmp4:
		return streamBitrate(mp, i), true
	case ch > 2 && aac51:
		return aac51BitRate, false
	}
	return 128 * parseBitrate(s.SampleRate) / 44, false
}

// aac51BitRate and aacConfigLayouts are content-transcoder's (services/
// audio.go): the rate it encodes AAC 5.1 at, and the channel layouts, as
// ffprobe names them, of the AAC configurations it copies under aac51.
const aac51BitRate = 384_000

var aacConfigLayouts = map[string]bool{"3.0": true, "4.0": true, "5.0": true, "5.1": true}

// encodedVideoCeiling is the most the transcoder's re-encode of a video
// height pixels tall pulls on average, 0 for a height not known. It
// encodes at a quality target (-crf 20) under a VBV cap of 1.3 times a
// rate it sets by the height (content-transcoder services/hls.go,
// sha-22f64b9: codecParams' -maxrate, Rendition.Rate interpolating
// DefaultRenditions; its float arithmetic kept). Measured with its FFmpeg
// 8.1.2 on noise, the worst case for a quality target (2026-10-03, 90 s,
// 4 s TS segments): 480p came to 1.03 times -maxrate on average and 1.11
// at most a segment, 360p to 1.045 and 1.14 -- within statusview.FitsMargin.
func encodedVideoCeiling(height int) int64 {
	h := uint(height)
	var hl, rl uint
	rate := uint(8000)
	for _, r := range [][2]uint{{240, 500}, {360, 1000}, {480, 2500}, {720, 5000}, {1080, 8000}} {
		if h <= r[0] {
			rate = uint(float64(h-hl)/float64(r[0]-hl)*float64(r[1]-rl)) + rl
			break
		}
		hl, rl = r[0], r[1]
	}
	return int64(uint(float64(rate)*1.3)) * 1000
}

// startAudio is which of the probe's audio streams (its place among them)
// the player starts on: the one the picker marks default
// (handlers/action Helper.GetAudioTracks, rendered data-default, which
// hls-manager.js starts on). A copy of that rule, for that package imports
// this one; TestStartAudio_IsThePickersDefault holds the two to one
// answer. A choice carried over from the previous file, by its language and
// label; else the saved one; else the viewer's language (the resolved one,
// then the browser's), then the fallback language; else the first.
func startAudio(mp *api.MediaProbe, ud *models.VideoStreamUserData) int {
	if ud == nil {
		return 0
	}
	type track struct{ lang, label string }
	var tracks []track
	for _, s := range mp.Streams {
		if s.CodecType != "audio" {
			continue
		}
		meta := ""
		if s.ChannelLayout != "" {
			meta = " (" + s.ChannelLayout + ")"
		}
		label := fmt.Sprintf("Audio%v #%v", meta, len(tracks)+1)
		if s.Tags.Title != "" {
			label = s.Tags.Title + meta
		}
		lang := s.Tags.Language
		if t, err := language.Parse(lang); err == nil {
			lang = t.String()
		}
		tracks = append(tracks, track{lang, label})
	}
	id := ud.AudioID
	if c := ud.Carry; c != nil && c.AudioLang != "" {
		carried := -1
		for i, t := range tracks {
			if !strings.EqualFold(t.lang, c.AudioLang) {
				continue
			}
			if c.AudioLabel != "" && t.label == c.AudioLabel {
				carried = i
				break
			}
			if carried < 0 {
				carried = i
			}
		}
		if carried >= 0 {
			id = "mp-" + strconv.Itoa(carried)
		}
	}
	for i := range tracks {
		if id == "mp-"+strconv.Itoa(i) {
			return i
		}
	}
	var langs []language.Tag
	at := map[language.Tag]int{}
	for i, t := range tracks {
		if tag, err := language.Parse(t.lang); err == nil {
			if _, ok := at[tag]; !ok {
				at[tag] = i
				langs = append(langs, tag)
			}
		}
	}
	want := ud.AcceptLangTags
	if ud.ResolvedLang != "" {
		if t, err := language.Parse(ud.ResolvedLang); err == nil {
			want = append([]language.Tag{t}, want...)
		}
	}
	m := language.NewMatcher(langs)
	for _, w := range [][]language.Tag{want, {ud.FallbackLangTag}} {
		if _, i, c := m.Match(w...); c > language.No {
			return at[langs[i]]
		}
	}
	return 0
}

// streamBitrate is the stream's own average bitrate: ffprobe's bit_rate, or
// mkvmerge's statistics tag where the container records no other (a
// Matroska video). 0 when it has none.
func streamBitrate(mp *api.MediaProbe, i int) int64 {
	s := mp.Streams[i]
	for _, v := range []string{s.BitRate, s.Tags.BPS, s.Tags.BPSEng} {
		if r := parseBitrate(v); r > 0 {
			return r
		}
	}
	return 0
}

func parseBitrate(v string) int64 {
	r, err := strconv.ParseInt(v, 10, 64)
	if err != nil || r < 0 {
		return 0
	}
	return r
}
