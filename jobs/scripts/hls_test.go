package scripts

import (
	"math"
	"testing"
)

func TestParseMasterVideoVariantURL(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="rus",NAME="Russian",URI="a0.m3u8?api-key=abc&token=xyz"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="eng",NAME="English",URI="a1.m3u8?api-key=abc&token=xyz"
#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=5000000,CODECS="avc1.42e00a,mp4a.40.2",AUDIO="audio"
v0-720.m3u8?api-key=abc&token=xyz`

	got, err := parseMasterVideoVariantURL(master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "v0-720.m3u8?api-key=abc&token=xyz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseMasterVideoVariantURL_NoVariant(t *testing.T) {
	body := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="eng",NAME="English",URI="a0.m3u8"
`
	_, err := parseMasterVideoVariantURL(body)
	if err == nil {
		t.Fatal("expected error for missing variant")
	}
}

func TestParseMediaPlaylist_WithSegments(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-TARGETDURATION:11
#EXTINF:10.051000,
v0-720-0.ts?api-key=abc&token=xyz
#EXTINF:8.758000,
v0-720-1.ts?api-key=abc&token=xyz
#EXTINF:9.500000,
v0-720-2.ts?api-key=abc&token=xyz
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endList {
		t.Error("expected endList=false")
	}
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0].URL != "v0-720-0.ts?api-key=abc&token=xyz" {
		t.Errorf("segment 0 URL = %q", segments[0].URL)
	}
	if math.Abs(segments[0].Duration-10.051) > 0.001 {
		t.Errorf("segment 0 duration = %f, want 10.051", segments[0].Duration)
	}
	if math.Abs(segments[1].Duration-8.758) > 0.001 {
		t.Errorf("segment 1 duration = %f, want 8.758", segments[1].Duration)
	}
}

func TestParseMediaPlaylist_WithEndList(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-TARGETDURATION:10
#EXTINF:10.000000,
seg0.ts
#EXTINF:5.000000,
seg1.ts
#EXT-X-ENDLIST
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !endList {
		t.Error("expected endList=true")
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
}

func TestParseMediaPlaylist_Empty(t *testing.T) {
	body := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-TARGETDURATION:10
`
	segments, endList, err := parseMediaPlaylist(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endList {
		t.Error("expected endList=false")
	}
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments, got %d", len(segments))
	}
}

func TestResolveURL_Relative(t *testing.T) {
	base := "https://example.com/stream/master.m3u8?api-key=abc&token=xyz"
	target := "v0-720.m3u8?api-key=abc&token=xyz"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com/stream/v0-720.m3u8?api-key=abc&token=xyz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveURL_Absolute(t *testing.T) {
	base := "https://example.com/stream/master.m3u8"
	target := "https://cdn.example.com/v0-720.m3u8?token=abc"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestResolveURL_RootRelative(t *testing.T) {
	base := "https://example.com/stream/master.m3u8"
	target := "/other/path.m3u8"
	got, err := resolveURL(base, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com/other/path.m3u8"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
