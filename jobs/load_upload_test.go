package j

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// A client gone by the time its upload is read and parsed gets no job:
// nothing is queued and nothing reaches rest-api.
func TestLoadSkipsGoneClient(t *testing.T) {
	torrent, h := privateTorrent(t)
	restAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
	})
	st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
	jobs := testJobsWith(t, restAPI, st)()
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	a := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: strings.Repeat("a", 40)}}
	for name, args := range map[string]*scripts.LoadArgs{
		"upload": {File: torrent, Source: "site"},
		"magnet": {Query: "magnet:?xt=urn:btih:" + h},
	} {
		j, err := jobs.Load(gone, a, args)
		if !errors.Is(err, context.Canceled) || j != nil {
			t.Errorf("%s: Load = %v, %v; want no job and context.Canceled", name, j, err)
		}
	}
	if _, ok := jobs.q.Get("load"); ok {
		t.Error("a load job was queued for a client that left")
	}
}

// The same bytes uploaded again by the same caller while the first run is in
// flight join that run: rest-api gets the file once. Other bytes or another
// caller run on their own, and once the run has ended an upload runs again.
func TestSameUploadJoinsRunInFlight(t *testing.T) {
	torrent, h := privateTorrent(t)
	// The same torrent (infohash, so the same load job id) in other bytes.
	mi, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		t.Fatal(err)
	}
	mi.Announce = "http://other.invalid/announce"
	var ob bytes.Buffer
	if err := mi.Write(&ob); err != nil {
		t.Fatal(err)
	}
	otherBytes := ob.Bytes()

	posts := make(chan []byte, 8)
	hold := make(chan struct{})
	restAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource" {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		b, _ := io.ReadAll(r.Body)
		posts <- b
		<-hold
		_ = json.NewEncoder(w).Encode(ra.ResourceResponse{ID: h})
	})
	st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
	jobs := testJobsWith(t, restAPI, st)()
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release) // before the server's Close, which waits for the held POSTs

	a := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: strings.Repeat("a", 40)}}
	b := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: strings.Repeat("b", 40)}}
	load := func(c *web.Context, file []byte) *job.Job {
		t.Helper()
		j, err := jobs.Load(context.Background(), c, &scripts.LoadArgs{File: bytes.Clone(file), Source: "site"})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	posted := func(what string, want []byte) {
		t.Helper()
		select {
		case got := <-posts:
			if !bytes.Equal(got, want) {
				t.Fatalf("%s: rest-api got other bytes", what)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no POST /resource", what)
		}
	}

	first := load(a, torrent)
	posted("first upload", torrent)
	if again := load(a, torrent); again != first {
		t.Error("the same upload by the same user started a second run")
	}
	if load(b, torrent) == first {
		t.Error("another user's upload joined the first user's run")
	}
	posted("another user", torrent)
	if load(a, otherBytes) == first {
		t.Error("other bytes of the same torrent joined the run")
	}
	posted("other bytes", otherBytes)
	select {
	case <-posts:
		t.Fatal("rest-api got the file more often than there are runs")
	default:
	}

	release()
	for range 3 {
		st.waitClosed(t, "held runs")
	}
	if load(a, torrent) == first {
		t.Error("an upload after the run ended joined the ended run")
	}
	posted("upload after the run", torrent)
	st.waitClosed(t, "upload after the run")
}
