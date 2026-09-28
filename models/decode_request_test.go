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
	} {
		if got := ParseDecodeRequest(c.decode, c.fb, c.class); got != c.want {
			t.Errorf("(%q,%q,%q): %+v, want %+v", c.decode, c.fb, c.class, got, c.want)
		}
	}
	for _, r := range []string{"codecs_rejected", "decode_error", "media_error", "src_unsupported", "no_frames", "user"} {
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
}
