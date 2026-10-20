package vault

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
)

// newTestVault is a Vault whose rest-api is h.
func newTestVault(t *testing.T, h http.HandlerFunc) *Vault {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return &Vault{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())}
}

// A hash rest-api does not know (its 404) is ErrResourceNotFound, not a nil
// dereference: an API client pledging to such a hash used to get a panic.
func TestRequiredVPOfUnknownResource(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	s := newTestVault(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	})

	if _, err := s.GetRequiredVP(context.Background(), &api.Claims{}, hash); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("GetRequiredVP err=%v, want ErrResourceNotFound", err)
	}
	if _, err := s.getTorrentName(context.Background(), &api.Claims{}, hash); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("getTorrentName err=%v, want ErrResourceNotFound", err)
	}
}

// rest-api's 404 is not cached: an API client stores a resource and pledges
// right after, so a miss read a moment before the store must not keep the
// hash unknown for the cache's minute.
func TestRequiredVPOfResourceStoredAfterMiss(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	var calls atomic.Int32
	s := newTestVault(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + hash + `","size":2147483648}`))
	})

	if _, err := s.GetRequiredVP(context.Background(), &api.Claims{}, hash); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("first GetRequiredVP err=%v, want ErrResourceNotFound", err)
	}
	vp, err := s.GetRequiredVP(context.Background(), &api.Claims{}, hash)
	if err != nil || vp != 2 {
		t.Fatalf("GetRequiredVP after the store = %v, %v; want 2, nil", vp, err)
	}
	// The hit is still cached.
	if _, err := s.getTorrentName(context.Background(), &api.Claims{}, hash); err != nil || calls.Load() != 2 {
		t.Fatalf("getTorrentName err=%v after %v rest-api calls, want nil after 2", err, calls.Load())
	}
}
