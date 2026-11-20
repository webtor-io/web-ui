package uploads

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/webtor-io/web-ui/services/api"
)

const delU = "9c5db5752b7c0123456789abcdef0123456789ab"

// fakeSeeder stands in for torrent-http-proxy and the seeder behind it on
// the service calls of the upload delU under prefix. It answers status, but
// first, as the pair does, 403 to a token that is not a service one for
// delU: no expiry, not internal, not up=add, another hash or no positive
// lim. method, when set, is the only method it takes: others get 405.
type fakeSeeder struct {
	t      *testing.T
	method string
	prefix string
	mu     sync.Mutex
	status int
	got    []string // "METHOD path?query" of every request
}

func startSeeder(t *testing.T, method, prefix string) (*fakeSeeder, *httptest.Server) {
	t.Helper()
	f := &fakeSeeder{t: t, method: method, prefix: prefix, status: http.StatusNoContent}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeSeeder) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.got = append(f.got, r.Method+" "+r.URL.RequestURI())
	status := f.status
	f.mu.Unlock()
	t := f.t
	if f.method != "" && r.Method != f.method {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if want := f.prefix + "/" + delU + "/"; r.URL.Path != want {
		t.Errorf("path %q, want %q", r.URL.Path, want)
	}
	q := r.URL.Query()
	if q.Get("api-key") != "k" {
		t.Errorf("api-key %q, want k", q.Get("api-key"))
	}
	if r.Method == http.MethodPut && (r.ContentLength != 0 || q.Has("offset") || q.Has("size")) {
		t.Errorf("seal: length %d, query %v; want no body, no offset or size", r.ContentLength, q)
	}
	c := &api.Claims{}
	if _, err := jwt.ParseWithClaims(q.Get("token"), c, func(*jwt.Token) (any, error) { return []byte("test"), nil }); err != nil {
		t.Errorf("token: %v", err)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if c.Uid != "" || c.SessionID != "" {
		t.Errorf("uid %q, session %q: a service token is no viewer's", c.Uid, c.SessionID)
	}
	if c.ExpiresAt == nil || !c.Internal || c.Up != "add" || c.Hash != delU || c.Lim <= 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if c.Lim != 1 {
		t.Errorf("lim %d, want 1", c.Lim)
	}
	w.WriteHeader(status)
}

func (f *fakeSeeder) set(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *fakeSeeder) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

// serviceOn is the uploads service and its api from these flags, on top of
// the secret test and the key k. The flags read the environment on Apply:
// every one that decides where a request goes is given.
func serviceOn(t *testing.T, args ...string) *Service {
	t.Helper()
	c := cliContext(t, append([]string{"--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key="}, args...)...)
	s := New(c, nil, api.New(c, http.DefaultClient))
	if s == nil {
		t.Fatal("uploads are off")
	}
	return s
}

func noSecrets(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), "token=") || strings.Contains(err.Error(), "api-key")) {
		t.Errorf("the error carries the URL: %v", err)
	}
}

func TestDelete(t *testing.T) {
	f, srv := startSeeder(t, http.MethodDelete, "")
	s := serviceOn(t, "--upload-url="+srv.URL, "--use-internal-torrent-http-proxy=false")
	for _, c := range []struct {
		status     int
		tombstoned bool
		fails      bool
	}{
		{http.StatusNoContent, true, false},
		{http.StatusGone, false, false},
		{http.StatusForbidden, false, true},
		{http.StatusNotFound, false, true},
		{http.StatusInternalServerError, false, true},
		{http.StatusInsufficientStorage, false, true},
	} {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			f.set(c.status)
			tombstoned, err := s.Delete(context.Background(), delU)
			if tombstoned != c.tombstoned || (err != nil) != c.fails {
				t.Errorf("%v, %v; want %v, an error %v", tombstoned, err, c.tombstoned, c.fails)
			}
			noSecrets(t, err)
		})
	}
}

func TestSeal(t *testing.T) {
	f, srv := startSeeder(t, http.MethodPut, "")
	s := serviceOn(t, "--upload-url="+srv.URL, "--use-internal-torrent-http-proxy=false")
	for _, c := range []struct {
		status int
		gone   bool
		fails  bool
	}{
		{http.StatusNoContent, false, false},
		{http.StatusGone, true, true},
		{http.StatusBadRequest, false, true},
		{http.StatusForbidden, false, true},
		{http.StatusNotFound, false, true},
		{http.StatusInternalServerError, false, true},
		{http.StatusInsufficientStorage, false, true},
	} {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			f.set(c.status)
			err := s.Seal(context.Background(), delU)
			if (err != nil) != c.fails || errors.Is(err, ErrGone) != c.gone {
				t.Errorf("%v; want an error %v, ErrGone %v", err, c.fails, c.gone)
			}
			noSecrets(t, err)
		})
	}
}

// web-ui's own calls to the seeder go where its other server-side calls to
// torrent-http-proxy go; UPLOAD_URL is the browser's address.
func TestServiceAddress(t *testing.T) {
	ctx := context.Background()
	f, srv := startSeeder(t, "", "/torrent-http-proxy")
	su, _ := url.Parse(srv.URL)
	thp := []string{"--torrent-http-proxy-host=" + su.Hostname(), "--torrent-http-proxy-port=" + su.Port()}

	both := func(t *testing.T, s *Service) {
		t.Helper()
		if tombstoned, err := s.Delete(ctx, delU); !tombstoned || err != nil {
			t.Errorf("delete: %v, %v; want true, nil", tombstoned, err)
		}
		if err := s.Seal(ctx, delU); err != nil {
			t.Errorf("seal: %v", err)
		}
	}

	t.Run("the internal address", func(t *testing.T) {
		before := len(f.requests())
		s := serviceOn(t, append([]string{"--upload-url=http://127.0.0.1:1/torrent-http-proxy", "--use-internal-torrent-http-proxy=true"}, thp...)...)
		both(t, s)
		got := f.requests()[before:]
		if len(got) != 2 || !strings.HasPrefix(got[0], "DELETE /torrent-http-proxy/"+delU+"/?") || !strings.HasPrefix(got[1], "PUT /torrent-http-proxy/"+delU+"/?") {
			t.Errorf("requests %v", got)
		}

		before = len(f.requests())
		g, err := s.Grant(&api.Claims{}, delU, "new", 1, 1, 0, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if g.URL != "http://127.0.0.1:1/torrent-http-proxy/"+delU {
			t.Errorf("the browser gets %q, want UPLOAD_URL", g.URL)
		}
		if n := len(f.requests()) - before; n != 0 {
			t.Errorf("%d requests", n)
		}
	})

	t.Run("no internal address", func(t *testing.T) {
		before := len(f.requests())
		both(t, serviceOn(t, "--upload-url="+srv.URL+"/torrent-http-proxy", "--use-internal-torrent-http-proxy=false",
			"--torrent-http-proxy-host=127.0.0.1", "--torrent-http-proxy-port=1"))
		if n := len(f.requests()) - before; n != 2 {
			t.Errorf("%d requests, want 2", n)
		}
	})

	for _, internal := range []string{"true", "false"} {
		name := map[string]string{"true": "internal", "false": "direct"}[internal]
		t.Run("a broken address, "+name, func(t *testing.T) {
			before := len(f.requests())
			s := serviceOn(t, append([]string{"--upload-url=http://%zz", "--use-internal-torrent-http-proxy=" + internal}, thp...)...)
			_, err := s.Delete(ctx, delU)
			serr := s.Seal(ctx, delU)
			for _, e := range []error{err, serr} {
				if e == nil || !strings.Contains(e.Error(), "invalid URL escape") {
					t.Errorf("%v, want a parse error", e)
				}
				noSecrets(t, e)
			}
			if n := len(f.requests()) - before; n != 0 {
				t.Errorf("%d requests", n)
			}
		})
	}
}
