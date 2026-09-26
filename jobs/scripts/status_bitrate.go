package scripts

import (
	"strconv"

	"github.com/webtor-io/web-ui/services/api"
)

// playedBitrate is the bitrate, in bits a second, of what the viewer's player
// pulls for the probed file, or 0 when that is not known. The transfer
// status's marks (StatusFitsCap, StatusOverCap) and its "…and this file
// needs N Mbps" compare it with the viewer's cap; a file whose number is not
// known is marked neither way, and its stream box waits for a real stall.
// It is an estimate of the tracks' average; what crosses thp is MPEG-TS
// segments of a stretch of the film: the transcoder's AAC came to ~236
// kbit/s in TS against the 139.6 below, and a 720p file's first 132 s to
// 1.16 times its estimate in all (statusview.FitsMargin, which "fits"
// keeps clear of).
//
// It is not the file's own bitrate. The player pulls one video track and one
// audio track: the transcoder's HLS (content-transcoder, Online mode) copies
// H.264 video as it is, re-encodes any other video, and serves each audio
// track as a rendition of its own -- re-encoded to stereo AAC unless it
// already is -- of which the player fetches one; nginx-vod's HLS of an
// mp4 repackages its first video and audio tracks as they are. The file's
// rate (ffprobe's format bit_rate) counts every dub, commentary and lossless
// track besides: a 3.5 Mbps H.264 with two 1.5 Mbps DTS dubs reads 6.6 and
// would be sold a faster plan while it plays smoothly at 5 (24 h of transcoder
// probes, 2026-09-26: of 202 H.264 files over a 5 Mbps cap by the file's
// rate, 36 are under it by the stream's and 14 have no number to tell).
//
// transcoded is the stream export's own word (ExportMeta.Transcode): the
// file goes through the transcoder, else it is served as it is (nginx-vod's
// HLS for a video, the file itself for audio).
func playedBitrate(mp *api.MediaProbe, transcoded bool) int64 {
	if mp == nil {
		return 0
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
	// Statistics tags a later remux copied unchanged describe another
	// file: a 720p re-encode at 0.7 Mbps still said 7.8 for its video. The
	// streams of a file cannot add up to more than the file: of 767 files
	// whose video and audio all have a number, 741 add up to 0.9-1.02 of
	// it, 25 (stale) to 1.1 and more, one in between.
	if file > 0 {
		var sum int64
		if video >= 0 {
			sum += own(video)
		}
		for _, a := range audio {
			sum += own(a)
		}
		if sum*100 > file*105 {
			return 0
		}
	}
	if video < 0 {
		if len(audio) == 0 {
			return 0
		}
		if !transcoded {
			// The file itself.
			return file
		}
		return audioOut(mp, audio[0], transcoded)
	}
	if transcoded && mp.Streams[video].CodecName != "h264" {
		// Re-encoded at a quality target (CRF): the rate is the encoder's
		// choice, known only once it is made.
		return 0
	}
	v := own(video)
	if v == 0 {
		// No number of its own: the file less its audio, when every audio
		// track has one. What is left besides the video -- subtitles,
		// the container's own bytes -- is a percent or two.
		if file == 0 {
			return 0
		}
		v = file
		for _, a := range audio {
			r := own(a)
			if r == 0 {
				return 0
			}
			v -= r
		}
		if v <= 0 {
			return 0
		}
	}
	if len(audio) == 0 {
		return v
	}
	// The first audio track: the one the player starts on unless the
	// viewer's language picks another, whose rate differs by a fraction of
	// a megabit (all re-encoded ones are the same).
	a := audioOut(mp, audio[0], transcoded)
	if a == 0 {
		return 0
	}
	return v + a
}

// audioOut is the bitrate the player pulls for the audio stream i: the
// transcoder re-encodes any audio that is not stereo AAC (content-transcoder
// codecParams: aac with at most two channels is copied) to two channels
// with libfdk_aac at its default rate, which FFmpeg sets as 128 kbit per
// channel pair times the sample rate over 44 kHz (libfdk-aacenc.c: 139.6
// kbps at 48 kHz); everything else is served as it is. 0 when not known.
func audioOut(mp *api.MediaProbe, i int, transcoded bool) int64 {
	s := mp.Streams[i]
	if transcoded && (s.CodecName != "aac" || s.Channels > 2) {
		sr := parseBitrate(s.SampleRate)
		if sr == 0 {
			return 0
		}
		return 128 * sr / 44
	}
	return streamBitrate(mp, i)
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
