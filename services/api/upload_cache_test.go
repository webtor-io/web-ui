package api

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli"
)

// An upload's files change while its owner adds or deletes them: its page and
// the enrichment that runs on a change read rest-api, not a minute-old answer.
// A torrent's answers are cached as before.
func TestUploadsBypassTheMinuteCaches(t *testing.T) {
	const (
		u0 = "9c5db5752b7c0123456789abcdef0123456789ab"
		tt = "0123456789abcdef0123456789abcdef01234567"
	)
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/list") {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/resource/")
		_, _ = w.Write([]byte(`{"id":"` + id + `","name":"n","private":true}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-rest-api-secure=false", "--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	s := New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
	ctx := context.Background()
	count := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return seen[path]
	}

	for _, c := range []struct {
		id   string
		want int
	}{{u0, 2}, {tt, 1}} {
		for range 2 {
			if r, err := s.GetResourceCached(ctx, InternalClaims(), c.id); err != nil || r == nil || r.ID != c.id {
				t.Fatalf("GetResourceCached %s: %v, %v", c.id, r, err)
			}
		}
		if n := count("/resource/" + c.id); n != c.want {
			t.Errorf("GetResourceCached %s: %d requests, want %d", c.id, n, c.want)
		}
		for range 2 {
			if _, err := s.ListResourceContentCached(ctx, InternalClaims(), c.id, &ListResourceContentArgs{}); err != nil {
				t.Fatalf("ListResourceContentCached %s: %v", c.id, err)
			}
		}
		if n := count("/resource/" + c.id + "/list"); n != c.want {
			t.Errorf("ListResourceContentCached %s: %d requests, want %d", c.id, n, c.want)
		}
	}
}
