package libfs

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vfs"
	"github.com/webtor-io/web-ui/services/web"
	"github.com/webtor-io/web-ui/services/webdav"
)

// libraryEntryFS serves one library entry through TorrentLibraryDirectory's
// real Stat and Open conversions, minus the database lookup by name.
type libraryEntryFS struct {
	*TorrentLibraryDirectory
	l *models.Library
}

func (f libraryEntryFS) Stat(context.Context, string) (*vfs.FileInfo, error) {
	fi := f.libraryToFileInfo(f.l)
	return &fi, nil
}

func (f libraryEntryFS) Open(ctx context.Context, _ string) (io.ReadCloser, *url.URL, error) {
	return f.readTorrent(ctx, f.l)
}

// The .torrent bytes rest-api serves drift from torrent_size_bytes recorded at
// upload: torrent-store merges announce lists into the record (it grows) and
// serves private torrents without trackers (it shrinks). A WebDAV GET must
// send the bytes it has with their own length: declaring the stored size
// broke both cases off with an unexpected EOF.
func TestWebDAVGetTorrentSendsActualBytes(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	actual := bytes.Repeat([]byte("d8:announce31:http://tracker.invalid/announcee"), 10)

	restAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash+".torrent" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(actual)
	}))
	t.Cleanup(restAPI.Close)
	u, _ := url.Parse(restAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	dir := &TorrentLibraryDirectory{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client())}

	for name, stored := range map[string]int64{
		"stored size smaller (merged record grew)":   int64(len(actual)) - 100,
		"stored size larger (private record shrank)": int64(len(actual)) + 100,
	} {
		t.Run(name, func(t *testing.T) {
			l := &models.Library{
				Name:      "probe",
				CreatedAt: time.Unix(1700000000, 0),
				Torrent:   &models.TorrentResource{ResourceID: hash, TorrentSizeBytes: stored},
			}
			h := &webdav.Handler{FileSystem: libraryEntryFS{TorrentLibraryDirectory: dir, l: l}}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wc := &web.Context{ApiClaims: &api.Claims{}}
				h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), web.Context{}, wc)))
			}))
			t.Cleanup(srv.Close)

			resp, err := http.Get(srv.URL + "/probe.torrent")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatalf("GET body: %v (Content-Length %d, actual %d)", err, resp.ContentLength, len(actual))
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET status = %d", resp.StatusCode)
			}
			if !bytes.Equal(body, actual) {
				t.Errorf("GET body = %d bytes, want the %d bytes rest-api served", len(body), len(actual))
			}
			if resp.ContentLength != int64(len(actual)) {
				t.Errorf("GET Content-Length = %d, want %d", resp.ContentLength, len(actual))
			}

			// HEAD goes through the same Open and must declare the same length.
			resp, err = http.Head(srv.URL + "/probe.torrent")
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(actual)) {
				t.Errorf("HEAD Content-Length = %s, want %d", got, len(actual))
			}
		})
	}
}

// A private .torrent put into the library over WebDAV or S3 is counted once,
// by the protocol it came through (services/private_torrent): the tree is
// shared, so the name is the one the protocol gave New.
func TestStoreTorrentCountsAPrivateUpload(t *testing.T) {
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "udp://tracker.invalid:6969/" + "0123456789abcdef0123456789abcdef" + "/announce"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	hash := mi.HashInfoBytes().HexString()
	restAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + hash + `"}`))
	}))
	t.Cleanup(restAPI.Close)
	u, _ := url.Parse(restAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	sapi := api.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client())
	id := uuid.NewV4()
	ctx := context.WithValue(context.Background(), web.Context{}, &web.Context{ApiClaims: &api.Claims{}, User: &auth.User{ID: id}})

	for _, source := range []string{"webdav", "s3"} {
		hook := test.NewGlobal()
		dir := New(nil, sapi, nil, source).(*DebugDirectory).Inner.(*RootDirectory).Children[RootTorrents].(*TorrentLibraryDirectory)
		if _, _, err := dir.storeTorrentToAPI(ctx, torrent.Bytes()); err != nil {
			t.Fatal(err)
		}
		var lines []map[string]any
		for _, e := range hook.AllEntries() {
			if e.Data["event"] == "private_torrent_upload" {
				lines = append(lines, e.Data)
			}
		}
		hook.Reset()
		if len(lines) != 1 || lines[0]["source"] != source || lines[0]["user"] != auth.LogHash(id) || lines[0]["trackers"] != "udp://tracker.invalid:6969" {
			t.Errorf("%s: lines %v, want one", source, lines)
		}
	}
}

// libfsAPI is an Api whose rest-api is h.
func libfsAPI(t *testing.T, h http.HandlerFunc) *api.Api {
	t.Helper()
	restAPI := httptest.NewServer(h)
	t.Cleanup(restAPI.Close)
	u, _ := url.Parse(restAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client())
}

// privateTorrent is an upload of a private torrent and its infohash.
func privateTorrent(t *testing.T) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	return torrent.Bytes(), mi.HashInfoBytes().HexString()
}

// rest-api serves a private torrent's .torrent to no user: a WebDAV GET of it
// answers rest-api's 403, not a 500 that reads as webtor being broken.
func TestWebDAVGetPrivateTorrentIs403(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	dir := &TorrentLibraryDirectory{api: libfsAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash+".torrent" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent_file"}`))
	})}
	l := &models.Library{
		Name:      "probe",
		CreatedAt: time.Unix(1700000000, 0),
		Torrent:   &models.TorrentResource{ResourceID: hash, TorrentSizeBytes: 100},
	}
	h := &webdav.Handler{FileSystem: libraryEntryFS{TorrentLibraryDirectory: dir, l: l}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wc := &web.Context{ApiClaims: &api.Claims{}}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), web.Context{}, wc)))
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/probe.torrent")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET status = %d, want 403", resp.StatusCode)
	}
}

// A private torrent's .torrent is served to no user, so its size is not read
// back after an upload: the request would only be refused (and the row would
// fall back to the upload's size anyway, with a warning).
func TestCreatePrivateSkipsSizeRead(t *testing.T) {
	torrent, hash := privateTorrent(t)
	var asked atomic.Bool
	dir := &TorrentLibraryDirectory{pg: &cs.PG{}, api: libfsAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			_, _ = w.Write([]byte(`{"id":"` + hash + `","private":true}`))
		case r.URL.Path == "/resource/"+hash+".torrent":
			asked.Store(true)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent_file"}`))
		default:
			http.NotFound(w, r)
		}
	})}
	ctx := context.WithValue(context.Background(), web.Context{}, &web.Context{ApiClaims: &api.Claims{}, User: &auth.User{ID: uuid.NewV4()}})

	// No database here: Create gets as far as the row, which is past the size.
	_, _, err := dir.Create(ctx, "/probe.torrent", io.NopCloser(bytes.NewReader(torrent)), nil)
	if err == nil || err.Error() != "db is nil" {
		t.Fatalf("err = %v, want the row's missing database", err)
	}
	if asked.Load() {
		t.Error("the .torrent of a private torrent was asked for")
	}
}

// rest-api's refusal of an upload reaches WebDAV and S3 with its own status,
// 422, not as a 500.
func TestCreatePrivateRefusedIs422(t *testing.T) {
	torrent, _ := privateTorrent(t)
	dir := &TorrentLibraryDirectory{pg: &cs.PG{}, api: libfsAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"x","code":"private_no_tracker"}`))
	})}
	ctx := context.WithValue(context.Background(), web.Context{}, &web.Context{ApiClaims: &api.Claims{}, User: &auth.User{ID: uuid.NewV4()}})

	_, _, err := dir.Create(ctx, "/probe.torrent", io.NopCloser(bytes.NewReader(torrent)), nil)
	if got := vfs.StatusCode(err); got != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422: %v", got, err)
	}
}
