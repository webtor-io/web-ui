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
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
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
