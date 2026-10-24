package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A non-2xx probe answer must surface as its status, not as a JSON parse
// error of the body: for six months every probe failure in the stream job
// read "failed to unmarshal data=404 page not found".
func TestGetMediaProbe_NonOKStatusIsTheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a := &Api{cl: srv.Client()}
	_, err := a.GetMediaProbe(context.Background(), srv.URL+"/x~cp")
	if err == nil {
		t.Fatal("expected an error for a 404 probe")
	}
	if !strings.Contains(err.Error(), "status=404") {
		t.Errorf("error should carry the status, got: %v", err)
	}
	if strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("a 404 must not be reported as a JSON error, got: %v", err)
	}
}

// An empty or non-RFC 3339 creation_time (CREATION_TIME in some files) used
// to fail the whole probe: the tag was decoded into a time.Time nobody read.
// On 2026-10-23 one release with an empty tag failed 252 jobs.
func TestGetMediaProbe_OddCreationTimeIsIgnored(t *testing.T) {
	body := `{"format":{"duration":"3304.301000","tags":{"CREATION_TIME":""}},"streams":[{"codec_name":"h264","width":1920,"tags":{"creation_time":"2019-05-09 22:16:45"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	a := &Api{cl: srv.Client()}
	mp, err := a.GetMediaProbe(context.Background(), srv.URL+"/x~cp")
	if err != nil {
		t.Fatal(err)
	}
	if mp.Format.Duration != "3304.301000" || len(mp.Streams) != 1 || mp.Streams[0].Width != 1920 {
		t.Errorf("media fields lost: %+v", mp)
	}
}
