package thumbnail

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
