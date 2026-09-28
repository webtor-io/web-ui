package scripts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// A restart after a passthrough failed in the browser carries its reason
// (decode-fallback); when the transcoder then refuses the file -- it
// declares nothing for it, so the refusal names no_declaration -- the job
// marks the refusal Fallback and the viewer reads that this browser could
// not show the 4K film, not that 4K is not streamed. A start that is not a
// restart reads the old text, and so does a restart after the file's
// multichannel audio failed (an audio class): it still declares its video,
// so what the transcoder says is the route's own word, not "this browser
// could not show it".
func TestBufferSessionHLSMarksAFallbackRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Video-Route-Reason", "no_declaration")
		http.Error(w, "resolution over 1080p is not supported", http.StatusUnsupportedMediaType)
	}))
	defer srv.Close()
	s := &ActionScript{api: directAPI(t), c: &web.Context{Lang: "en"}, i18n: i18n.New(os.DirFS("../../locales")), warmup: WarmupSettings{TimeoutMin: 1}}
	for _, c := range []struct {
		decl     models.DecodeRequest
		fallback bool
		key      string
	}{
		{models.DecodeRequest{FallbackReason: "decode_error", FallbackClass: "hevc10-2160"}, true, "error.video_route.fallback_uhd"},
		{models.DecodeRequest{}, false, "error.resolution_not_supported"},
		{models.DecodeRequest{Decode: "hevc8,hevc10,aac51", FallbackReason: "decode_error", FallbackClass: "dolby"}, false, "error.resolution_not_supported"},
		{models.DecodeRequest{FallbackReason: "media_error", FallbackClass: "aac51"}, false, "error.resolution_not_supported"},
	} {
		j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
		_, err := s.bufferSessionHLS(context.Background(), j, srv.URL+"/abc/f.mkv~hls/index.m3u8", time.Second, c.decl)
		var tr *api.TranscoderRefusal
		if !errors.As(err, &tr) || tr.Fallback != c.fallback {
			t.Fatalf("%+v: %v (refusal %+v)", c.decl, err, tr)
		}
		if got := web.ClassifyError(errors.Wrap(err, "failed to buffer session HLS")); got != c.key {
			t.Errorf("%+v: %s, want %s", c.decl, got, c.key)
		}
	}
}
