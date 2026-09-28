package models

import "strings"

// DecodeRequest is what a stream start says about the viewer's browser for
// the HEVC passthrough and multichannel audio (docs/player.md, "The
// declaration"): which decoders it has, and -- on a restart after a
// passthrough that failed in this browser -- why it failed. The zero value is
// a start that declares nothing, which is every start of a browser that has
// not opted in: the transcoder then takes the route it always took.
//
// It is read from the request, never from the session cookie: one account
// watches on several devices, and each declares for itself.
type DecodeRequest struct {
	// Decode is the declaration in canonical form (ParseDecodeDeclaration):
	// the transcoder's "decode" query value, "" for none.
	Decode string
	// FallbackReason is set on a restart after a failed passthrough
	// (ParseFallbackReason); "" on any other start.
	FallbackReason string
	// FallbackClass is the decoder class that failed (ParseFallbackClass),
	// for the fallback counter; "" without a FallbackReason.
	FallbackClass string
}

// Key is the part of a job's cache key that keeps renders for different
// declarations apart. "" for the zero value, so a start that declares
// nothing keeps the job id it has always had. A declaration of audio tokens
// only is a declaration like any other: its render holds a transcoder
// session whose audio may be 5.1 or Dolby, which a browser that did not
// declare them must not be served.
func (d DecodeRequest) Key() string {
	k := ""
	if d.Decode != "" {
		k += "/decode=" + d.Decode
	}
	if d.FallbackReason != "" {
		k += "/fb=" + d.FallbackReason + ":" + d.FallbackClass
	}
	return k
}

// The declaration's tokens in the transcoder's order (content-transcoder
// services/route.go knownDecodeTokens): what a browser decodes. Renaming one
// is a protocol change.
//
// The video tokens decide the video route (HEVC passthrough); the audio
// tokens -- AAC with up to 6 channels, AC-3, E-AC-3 -- decide only what the
// transcoder does with multichannel audio, and come after the video ones. A
// transcoder that does not know a token ignores it, so the audio tokens may
// reach one that predates them: it reads a declaration of audio tokens
// alone as no declaration, the route it has always taken.
var (
	videoDecodeTokens = []string{"hevc8", "hevc10", "hevc8-2160", "hevc10-2160", "hevc-high", "hdr-pq"}
	audioDecodeTokens = []string{"aac51", "ac3", "ec3"}
	decodeTokens      = append(append([]string{}, videoDecodeTokens...), audioDecodeTokens...)
)

// DecodeUnknown is the declaration of a page whose check had not answered
// when the form was sent: the transcoder asks for another try on what it
// would have to refuse, rather than refusing it.
const DecodeUnknown = "unknown"

// maxDecodeDeclaration bounds what is looked at, as the transcoder does: a
// real declaration is under 80 bytes.
const maxDecodeDeclaration = 512

// ParseDecodeDeclaration reads a browser's "decode" field the way the
// transcoder reads its query parameter: comma-separated tokens, each matched
// exactly (surrounding spaces trimmed) against the allowlist, unknown ones
// ignored. The result is canonical -- known tokens in the transcoder's order,
// no duplicates -- so the same declaration makes the same job key.
//
// "unknown" says the page's check of the video decoders had not answered.
// With a video token it is dropped: whatever answered is an answer. Without
// one it stays, alone -- audio tokens beside it are dropped, never sent in
// its place: a declaration of audio tokens only is one that answered "no
// HEVC", and the transcoder would refuse a 4K HEVC file for a decoder the
// browser was still being asked about. The page never sends the two
// together (decode-declaration.js); this keeps any other client to the same
// rule. Nothing known, or an over-long value, is "".
func ParseDecodeDeclaration(v string) string {
	if len(v) > maxDecodeDeclaration {
		return ""
	}
	seen := map[string]bool{}
	unknown := false
	for _, t := range strings.Split(v, ",") {
		t = strings.TrimSpace(t)
		if t == DecodeUnknown {
			unknown = true
			continue
		}
		seen[t] = true
	}
	video := false
	for _, t := range videoDecodeTokens {
		video = video || seen[t]
	}
	if unknown && !video {
		return DecodeUnknown
	}
	var out []string
	for _, t := range decodeTokens {
		if seen[t] {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// Why a passthrough was given up in the browser (docs/player.md, "Passthrough:
// errors and fallback"). A closed set: the value becomes a metric label.
var fallbackReasons = map[string]bool{
	"codecs_rejected": true, // the browser refused the stream's codec string
	"decode_error":    true, // the element reported a decoder error
	"media_error":     true, // a media error that survived one recovery
	"src_unsupported": true, // the element refused the source
	"no_frames":       true, // time moved, no picture
	"user":            true, // the viewer chose the compatible mode
}

// ParseFallbackReason is the "decode-fallback" field of a restart, or "".
func ParseFallbackReason(v string) string {
	if fallbackReasons[v] {
		return v
	}
	return ""
}

// fallbackClasses are the decoder classes a failure is charged to: the
// HEVC tokens a stream needs by depth and size (hevc-high and hdr-pq are
// never charged -- the page cannot tell a tier or a PQ failure from a size
// one), and the audio classes (audioFallbackClasses).
var fallbackClasses = map[string]bool{"hevc8": true, "hevc10": true, "hevc8-2160": true, "hevc10-2160": true, "dolby": true, "aac51": true}

// audioFallbackClasses are the classes of a restart after the multichannel
// audio a declaration made failed in the browser (docs/player.md,
// "Multichannel audio and the fallback"): "dolby" -- AC-3/E-AC-3 copied as it
// is; "aac51" -- AAC with more than two channels. Such a restart still
// declares the rest -- the video tokens with it -- so it is not the old
// route's restart of a video that could not be shown.
var audioFallbackClasses = map[string]bool{"dolby": true, "aac51": true}

// ParseFallbackClass is the "decode-class" field of a restart: a class, or
// "unknown" for anything else.
func ParseFallbackClass(v string) string {
	if fallbackClasses[v] {
		return v
	}
	return DecodeUnknown
}

// IsAudioFallback: this start restarts a file whose multichannel audio
// failed in the browser, not one whose video did.
func (d DecodeRequest) IsAudioFallback() bool {
	return d.FallbackReason != "" && audioFallbackClasses[d.FallbackClass]
}

// DeclaresAudio: the declaration carries an audio token, so the
// transcoder may have made the session's audio other than the stereo AAC it
// has always made.
func (d DecodeRequest) DeclaresAudio() bool {
	for _, t := range strings.Split(d.Decode, ",") {
		for _, a := range audioDecodeTokens {
			if t == a {
				return true
			}
		}
	}
	return false
}

// ParseDecodeRequest reads the three fields of a stream start. The class is
// kept only with a reason.
func ParseDecodeRequest(decode, fallback, class string) DecodeRequest {
	d := DecodeRequest{
		Decode:         ParseDecodeDeclaration(decode),
		FallbackReason: ParseFallbackReason(fallback),
	}
	if d.FallbackReason != "" {
		d.FallbackClass = ParseFallbackClass(class)
	}
	return d
}
