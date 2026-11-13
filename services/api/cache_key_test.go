package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
)

const cacheTestHash = "0123456789abcdef0123456789abcdef01234567"

func TestCacheKey(t *testing.T) {
	const h = cacheTestHash
	uid := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		c    *Claims
		want string
	}{
		// No api middleware (error pages): no claims at all.
		{"nil", nil, "anon/" + h},
		{"anonymous", &Claims{}, "anon/" + h},
		{"signed in", &Claims{Uid: uid}, "u/" + uid + "/" + h},
		{"internal", &Claims{Internal: true}, "internal/" + h},
		{"internal with uid", &Claims{Internal: true, Uid: uid}, "internal/" + h},
	} {
		if got := cacheKey(tc.c, h); got != tc.want {
			t.Errorf("%s: cacheKey = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// callerClass is how the fake rest-api tells callers apart: by the internal
// and uid claims of the token.
func callerClass(c *Claims) string {
	switch {
	case c == nil:
		return "anon"
	case c.Internal:
		return "internal"
	case c.Uid != "":
		return "u/" + c.Uid
	}
	return "anon"
}

// A reads, then B; internal reads, then an anonymous caller: the second never
// gets the first one's answer from the cache, and a caller's repeat is still a hit.
func TestCachedReadsAreKeyedByCaller(t *testing.T) {
	const h = cacheTestHash
	ctx := context.Background()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, m := range []struct {
		name string
		get  func(s *Api, c *Claims) (string, error)
	}{
		{"GetResourceCached", func(s *Api, c *Claims) (string, error) {
			r, err := s.GetResourceCached(ctx, c, h)
			if r == nil {
				return "", err
			}
			return r.ID, err
		}},
		{"ListResourceContentCached", func(s *Api, c *Claims) (string, error) {
			r, err := s.ListResourceContentCached(ctx, c, h, &ListResourceContentArgs{})
			if r == nil {
				return "", err
			}
			return r.ID, err
		}},
		{"GetTorrentCached", func(s *Api, c *Claims) (string, error) {
			d, err := s.GetTorrentCached(ctx, c, h)
			return string(d), err
		}},
	} {
		t.Run(m.name, func(t *testing.T) {
			var mu sync.Mutex
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hits++
				mu.Unlock()
				class := r.Header.Get("X-Test-Caller")
				if strings.HasSuffix(r.URL.Path, ".torrent") {
					_, _ = w.Write([]byte(class))
					return
				}
				_ = json.NewEncoder(w).Encode(ra.ListItem{ID: class})
			}))
			defer srv.Close()
			s := &Api{
				cl:  srv.Client(),
				url: srv.URL,
				prepareRequest: func(r *http.Request, c *Claims) (*http.Request, error) {
					r.Header.Set("X-Test-Caller", callerClass(c))
					return r, nil
				},
				torrentCache:      lazymap.New[[]byte](&lazymap.Config{Expire: time.Minute}),
				resourcesCache:    lazymap.New[*ra.ResourceResponse](&lazymap.Config{Expire: time.Minute}),
				listResponseCache: lazymap.New[*ra.ListResponse](&lazymap.Config{Expire: time.Minute}),
			}
			for i, step := range []struct {
				c    *Claims
				want string
				hits int
			}{
				{&Claims{Uid: a}, "u/" + a, 1},
				{&Claims{Uid: b}, "u/" + b, 2},
				{&Claims{Uid: a}, "u/" + a, 2}, // a repeat is still a hit
				{InternalClaims(), "internal", 3},
				{nil, "anon", 4},
				{&Claims{}, "anon", 4}, // no claims and empty claims are one class
			} {
				got, err := m.get(s, step.c)
				if err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				mu.Lock()
				n := hits
				mu.Unlock()
				if got != step.want || n != step.hits {
					t.Errorf("step %d: got %q after %d hits, want %q after %d", i, got, n, step.want, step.hits)
				}
			}
		})
	}
}
