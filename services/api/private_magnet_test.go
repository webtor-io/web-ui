package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rest-api answers a magnet that resolves to a private torrent it has not
// stored with 422, and stores nothing. The client names that answer by
// identity, from the status alone: other words under 422 are still it, and
// the same words under another status are not.
func TestStoreResourcePrivateMagnet(t *testing.T) {
	const contract = `{"error": "private torrent: this magnet has no tracker; upload the .torrent file or use a magnet that includes your tracker"}`
	for _, c := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"contract", http.StatusUnprocessableEntity, contract, true},
		{"other words", http.StatusUnprocessableEntity, `{"error":"placeholder"}`, true},
		{"no body", http.StatusUnprocessableEntity, "", true},
		{"same words, other status", http.StatusInternalServerError, contract, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/resource" {
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			a := &Api{
				cl:             srv.Client(),
				url:            srv.URL,
				prepareRequest: func(r *http.Request, _ *Claims) (*http.Request, error) { return r, nil },
			}
			res, err := a.StoreResource(context.Background(), &Claims{}, []byte("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"))
			if res != nil {
				t.Errorf("stored %+v, want nothing", res)
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if got := errors.Is(err, ErrPrivateMagnet); got != c.want {
				t.Errorf("errors.Is(%v, ErrPrivateMagnet) = %v, want %v", err, got, c.want)
			}
		})
	}
}
