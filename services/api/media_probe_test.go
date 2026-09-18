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
