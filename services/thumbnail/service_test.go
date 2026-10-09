package thumbnail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/api"
)

// The image's download URL is thp's, with the token and the api-key in its
// query; a transport error (*url.Error) quotes it whole, and the enrich run
// logs the error -- redacted, as services/api do() has it for every other
// call, the rest of the URL kept.
func TestFetchCapped_ErrorsDoNotQuoteTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL + "/abc/poster.jpg?api-key=KEY-SECRET&token=TOKEN-SECRET"
	srv.Close() // connection refused
	s := &Service{cl: http.DefaultClient}
	_, err := s.fetchCapped(context.Background(), u, MaxImageBytes)
	if err == nil {
		t.Fatal("want an error from a closed server")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error quotes a credential: %v", err)
	}
	if !strings.Contains(err.Error(), "/abc/poster.jpg") {
		t.Errorf("error lost the rest of the URL: %v", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("the cause is gone: %v", err)
	}
}

// ffmpeg runs in the web-ui container; without the cap a frame of an 8K
// video took the pod down (2026-10-09). Linux only: macOS refuses ulimit -v.
func TestRunFFmpeg_CapsAddressSpace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ulimit -v is set on Linux only")
	}
	defer func(b string) { ffmpegBin = b }(ffmpegBin)
	ffmpegBin = "sh"
	out, err := runFFmpeg(context.Background(), "-c", "ulimit -v")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != strconv.Itoa(ffmpegMaxVirtualKiB) {
		t.Errorf("ffmpeg runs with ulimit -v %s, want %d", got, ffmpegMaxVirtualKiB)
	}
}

// On a failed open ffmpeg quotes its input: thp's URL, token and api-key
// in the query.
func TestRunFFmpeg_ErrorsDoNotQuoteTheURL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ulimit -v is set on Linux only")
	}
	defer func(b string) { ffmpegBin = b }(ffmpegBin)
	ffmpegBin = "sh"
	_, err := runFFmpeg(context.Background(), "-c",
		`echo "https://thp/abc/a.mkv?token=TOKEN-SECRET&api-key=KEY-SECRET: Server returned 403 Forbidden" >&2; exit 1`)
	if err == nil {
		t.Fatal("want an error from a failed ffmpeg")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error quotes a credential: %v", err)
	}
	if !strings.Contains(err.Error(), "/abc/a.mkv") || !strings.Contains(err.Error(), "403") {
		t.Errorf("error lost the rest of ffmpeg's message: %v", err)
	}
}

func TestFramePixels(t *testing.T) {
	for _, c := range []struct {
		probe string
		over  bool
	}{
		{`{"streams":[{"codec_type":"video","width":8192,"height":4096},{"codec_type":"audio"}]}`, true},
		{`{"streams":[{"codec_type":"video","width":4096,"height":2160}]}`, false},
		{`{"streams":[{"codec_type":"video","width":3840,"height":2160}]}`, false},
		{`{"streams":[{"codec_type":"video","width":1920,"height":1080},{"codec_type":"video","width":7680,"height":4320}]}`, true},
		{`{"streams":[{"codec_type":"audio"}]}`, false},
	} {
		var mp api.MediaProbe
		if err := json.Unmarshal([]byte(c.probe), &mp); err != nil {
			t.Fatal(err)
		}
		if got := framePixels(&mp) > maxFramePixels; got != c.over {
			t.Errorf("%s: over %v, want %v", c.probe, got, c.over)
		}
	}
	if framePixels(nil) != 0 {
		t.Error("an unknown probe has frames")
	}
}
