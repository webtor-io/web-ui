package backends

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/api"
)

// A magnet of a private torrent is refused for good (rest-api's 422,
// api.ErrPrivateMagnet): EnsureResource says so to every caller, also to one
// whose own deadline ran out while it waited on another caller's store of the
// same hash. That one used to read its deadline as "still resolving", answer
// nil and queue a five-minute background store that can only be refused again.
func TestEnsureResourcePrivateMagnetIsFinal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	var posts atomic.Int32
	gets := make(chan struct{}, 8)
	arrived := make(chan struct{}, 8)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource/"+hash:
			w.WriteHeader(http.StatusNotFound)
			gets <- struct{}{}
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			posts.Add(1)
			arrived <- struct{}{}
			<-release
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error": "private torrent: this magnet has no tracker; upload the .torrent file or use a magnet that includes your tracker"}`))
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock() // before srv.Close, which waits for the handlers

	wait := func(ch chan struct{}, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s", what)
		}
	}
	s := NewWebtor(newTestAPI(t, srv))
	cl := &api.Claims{}

	first := make(chan error, 1)
	go func() { first <- s.EnsureResource(context.Background(), cl, hash) }()
	wait(gets, "lookup by the first caller")
	wait(arrived, "store by the first caller")

	// The second caller joins the store in flight and runs out of time there.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- s.EnsureResource(ctx, cl, hash) }()
	wait(gets, "lookup by the second caller")
	<-ctx.Done()
	unblock()

	for _, c := range []struct {
		name string
		ch   chan error
	}{{"first", first}, {"second", second}} {
		select {
		case err := <-c.ch:
			if !errors.Is(err, api.ErrPrivateMagnet) {
				t.Errorf("%s caller: %v, want api.ErrPrivateMagnet", c.name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s caller did not return", c.name)
		}
	}
	// A background store would have arrived by now.
	time.Sleep(300 * time.Millisecond)
	if n := posts.Load(); n != 1 {
		t.Errorf("%d stores, want 1: a refusal is not tried again", n)
	}
}

func newTestAPI(t *testing.T, srv *httptest.Server) *api.Api {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
}
