package vault

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
)

// A hash rest-api does not know (its 404) is ErrResourceNotFound, not a nil
// dereference: an API client pledging to such a hash used to get a panic.
func TestRequiredVPOfUnknownResource(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	s := &Vault{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())}

	if _, err := s.GetRequiredVP(context.Background(), &api.Claims{}, hash); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("GetRequiredVP err=%v, want ErrResourceNotFound", err)
	}
	if _, err := s.getTorrentName(context.Background(), &api.Claims{}, hash); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("getTorrentName err=%v, want ErrResourceNotFound", err)
	}
}
