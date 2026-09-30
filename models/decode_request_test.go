package models

import (
	"strings"
	"testing"
)

// The declaration is read as the transcoder reads it and written back in one
// canonical form: the same browser makes the same job key, and nothing but
// known tokens reaches the transcoder's query.
func TestParseDecodeDeclaration(t *testing.T) {
	for in, want := range map[string]string{
		"":                           "",
		"hevc8":                      "hevc8",
		"hdr-pq,hevc10-2160,hevc8":   "hevc8,hevc10-2160,hdr-pq",
		"hevc8,hevc8,hevc10":         "hevc8,hevc10",
		" hevc10 , hevc8 ":           "hevc8,hevc10",
		"hevc8,av1,HEVC10,hevc 10":   "hevc8",
		"unknown":                    "unknown",
		" unknown ":                  "unknown",
		"unknown,unknown":            "unknown",
		"unknown,hevc10":             "hevc10",
		"hevc-high,unknown,hdr-pq":   "hevc-high,hdr-pq",
		"garbage,<script>":           "",
		",,,":                        "",
		"hevc8;hevc10":               "",
		strings.Repeat("hevc8,", 86): "",
		strings.Repeat("hevc8,", 80): "hevc8",
		"hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq": "hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq",
	} {
		if got := ParseDecodeDeclaration(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The audio tokens (multichannel audio, 2026-09-28) reach the transcoder
// through the same allowlist, after the video ones. "unknown" -- the video
// check had not answered -- is dropped only by a video token: audio tokens
// alone would read there as a browser that answered "no HEVC".
func TestParseDecodeDeclarationAudio(t *testing.T) {
	for in, want := range map[string]string{
		"aac51":                               "aac51",
		"ec3,ac3,aac51":                       "aac51,ac3,ec3",
		"ec3,hdr-pq,aac51,hevc10":             "hevc10,hdr-pq,aac51,ec3",
		"aac51,aac51,ec3":                     "aac51,ec3",
		" ac3 ":                               "ac3",
		"AAC51,ac-3,ec-3,eac3,aac,dts,truehd": "",
		"unknown,aac51":                       "unknown",
		"aac51,ac3,ec3,unknown":               "unknown",
		"unknown,hevc8,aac51":                 "hevc8,aac51",
		"unknown,hdr-pq,ec3":                  "hdr-pq,ec3",
		"hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq,aac51,ac3,ec3": "hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq,aac51,ac3,ec3",
	} {
		if got := ParseDecodeDeclaration(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	// The order the page sends them in (codec-support.js DECODE_TOKENS)
	// is this one: the page's own declaration is already canonical.
	if got := strings.Join(decodeTokens, ","); got != "hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq,aac51,ac3,ec3" {
		t.Errorf("token order %s", got)
	}
}

func TestParseDecodeRequest(t *testing.T) {
	for _, c := range []struct {
		decode, fb, class string
		want              DecodeRequest
	}{
		{"", "", "", DecodeRequest{}},
		{"hevc10", "", "hevc10", DecodeRequest{Decode: "hevc10"}},
		{"", "decode_error", "hevc10-2160", DecodeRequest{FallbackReason: "decode_error", FallbackClass: "hevc10-2160"}},
		{"", "user", "", DecodeRequest{FallbackReason: "user", FallbackClass: "unknown"}},
		{"", "codecs_rejected", "hdr-pq", DecodeRequest{FallbackReason: "codecs_rejected", FallbackClass: "unknown"}},
		{"", "rm -rf", "hevc10", DecodeRequest{}},
		{"", "DECODE_ERROR", "hevc10", DecodeRequest{}},
		// A restart after the multichannel audio failed: the rest of the
		// declaration, and the audio class it is charged to.
		{"hevc8,hevc10,aac51", "decode_error", "dolby", DecodeRequest{Decode: "hevc8,hevc10,aac51", FallbackReason: "decode_error", FallbackClass: "dolby"}},
		{"", "media_error", "aac51", DecodeRequest{FallbackReason: "media_error", FallbackClass: "aac51"}},
		{"", "media_error", "Dolby", DecodeRequest{FallbackReason: "media_error", FallbackClass: "unknown"}},
	} {
		if got := ParseDecodeRequest(c.decode, c.fb, c.class); got != c.want {
			t.Errorf("(%q,%q,%q): %+v, want %+v", c.decode, c.fb, c.class, got, c.want)
		}
	}
	for _, r := range []string{"codecs_rejected", "decode_error", "media_error", "src_unsupported", "no_frames", "user", "fragment_loop"} {
		if ParseFallbackReason(r) != r {
			t.Errorf("reason %q is not allowed", r)
		}
	}
}

// The zero value adds nothing to a job key.
func TestDecodeRequestKey(t *testing.T) {
	if k := (DecodeRequest{}).Key(); k != "" {
		t.Errorf("zero value key %q", k)
	}
	if k := (DecodeRequest{Decode: "hevc8"}).Key(); k != "/decode=hevc8" {
		t.Errorf("key %q", k)
	}
	if k := (DecodeRequest{FallbackReason: "user", FallbackClass: "unknown"}).Key(); k != "/fb=user:unknown" {
		t.Errorf("key %q", k)
	}
	// Audio tokens alone are a declaration: their own key.
	if k := ParseDecodeRequest("aac51,ec3", "", "").Key(); k != "/decode=aac51,ec3" {
		t.Errorf("audio-only key %q", k)
	}
}

// An audio restart is told from a video one by its class, and only with a
// reason; an audio declaration by an audio token, matched exactly.
func TestDecodeRequestAudio(t *testing.T) {
	for _, c := range []struct {
		d     DecodeRequest
		audio bool
	}{
		{DecodeRequest{}, false},
		{DecodeRequest{FallbackReason: "decode_error", FallbackClass: "hevc10-2160"}, false},
		{DecodeRequest{FallbackReason: "decode_error", FallbackClass: "unknown"}, false},
		{DecodeRequest{FallbackReason: "decode_error", FallbackClass: "dolby"}, true},
		{DecodeRequest{FallbackReason: "media_error", FallbackClass: "aac51"}, true},
		{DecodeRequest{FallbackClass: "aac51"}, false},
	} {
		if got := c.d.IsAudioFallback(); got != c.audio {
			t.Errorf("%+v: IsAudioFallback %v", c.d, got)
		}
	}
	for _, c := range []struct {
		decode string
		want   bool
	}{
		{"", false},
		{"unknown", false},
		{"hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq", false},
		{"aac51", true},
		{"hevc8,ec3", true},
		{"ac3", true},
		{"aac5", false},
	} {
		if got := (DecodeRequest{Decode: c.decode}).DeclaresAudio(); got != c.want {
			t.Errorf("%q: DeclaresAudio %v", c.decode, got)
		}
	}
}
