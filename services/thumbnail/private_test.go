package thumbnail_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

const (
	testHash = "0123456789abcdef0123456789abcdef01234567"
	uidA     = "76543210fedcba9876543210fedcba9876543210" // holds a record of the torrent
	uidB     = "89abcdef0123456789abcdef0123456789abcdef" // holds none
)

// call is one request the fake rest-api got, with the claims of its token.
type call struct {
	path     string
	uid      string
	internal bool
}

// privateTorrent stands in for rest-api and torrent-http-proxy around one
// private torrent of a single image. web-ui's own lookup gets the resource;
// the listing and the export go to uidA only, and to no one else -- web-ui's
// own claims included, so a listing made as web-ui instead of as the viewer
// shows up as a refusal.
type privateTorrent struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []call
}

func startPrivateTorrent(t *testing.T, img []byte) *privateTorrent {
	t.Helper()
	f := &privateTorrent{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{path: r.URL.Path}
		if p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (any, error) { return []byte("test"), nil }); err == nil {
			mc := p.Claims.(jwt.MapClaims)
			c.internal = mc["internal"] == true
			c.uid, _ = mc["uid"].(string)
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		refuse := func() {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/resource/")
		switch {
		case r.URL.Path == "/blob/poster.jpg":
			_, _ = w.Write(img)
		case !ok:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		case strings.HasSuffix(rest, "/list"):
			if c.uid != uidA {
				refuse()
				return
			}
			_, _ = fmt.Fprintf(w, `{"items":[{"id":"i1","name":"poster.jpg","path":"/poster.jpg","type":"file","media_format":"image","size":%d}],"items_count":1}`, len(img))
		case strings.HasSuffix(rest, "/export/i1"):
			if c.uid != uidA {
				refuse()
				return
			}
			_, _ = fmt.Fprintf(w, `{"exports":{"download":{"url":%q}}}`, f.srv.URL+"/blob/poster.jpg")
		case c.internal:
			_, _ = fmt.Fprintf(w, `{"id":%q,"private":true}`, rest)
		default:
			refuse()
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// requests are the calls whose path ends with suffix.
func (f *privateTorrent) requests(suffix string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var cs []call
	for _, c := range f.calls {
		if strings.HasSuffix(c.path, suffix) {
			cs = append(cs, c)
		}
	}
	return cs
}

// api is a client of this rest-api with its own, empty caches.
func (f *privateTorrent) api(t *testing.T) *api.Api {
	t.Helper()
	u, _ := url.Parse(f.srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range api.RegisterFlags(nil) {
		fl.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), f.srv.Client())
}

func jpegOf(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A private torrent's picture is made as the viewer who plays it: the listing
// and the export go with the viewer's claims, and rest-api refuses a viewer
// without a right to it.
func TestGeneratePrivate(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Start(t)
	pg := pgtest.CS(t, db)
	img := jpegOf(t)
	sum := sha1.Sum(img)
	key := hex.EncodeToString(sum[:]) + ".jpg"
	h2 := testHash[:39] + "2"

	for _, c := range []struct {
		name  string
		uid   string
		hash  string
		check func(t *testing.T, f *privateTorrent, store *thumbtest.Store, err error)
	}{
		{"holder", uidA, testHash, func(t *testing.T, f *privateTorrent, store *thumbtest.Store, err error) {
			if err != nil {
				t.Fatalf("err = %v, want a thumbnail", err)
			}
			th, err := models.GetThumbnailByResourceID(ctx, db, testHash)
			if err != nil {
				t.Fatal(err)
			}
			if th == nil || th.SourceKind != models.ThumbnailSourceImageFile {
				t.Errorf("thumbnail row %+v, want one of an image file", th)
			}
			if keys := store.Keys("thumbs"); len(keys) != 1 || keys[0] != key {
				t.Errorf("objects %v, want [%s]", keys, key)
			}
			for _, suffix := range []string{"/list", "/export/i1"} {
				cs := f.requests(suffix)
				if len(cs) == 0 {
					t.Errorf("no %s request", suffix)
				}
				for _, c := range cs {
					if c.uid != uidA || c.internal {
						t.Errorf("%s asked as uid %q, internal %v; want the viewer's claims", c.path, c.uid, c.internal)
					}
				}
			}
		}},
		{"stranger", uidB, h2, func(t *testing.T, f *privateTorrent, store *thumbtest.Store, err error) {
			if err == nil || errors.Is(err, thumbnail.ErrNoSource) {
				t.Errorf("err = %v, want rest-api's refusal", err)
			}
			if n := len(f.requests("/export/i1")) + len(f.requests("/blob/poster.jpg")); n != 0 {
				t.Errorf("%d export and image requests, want none", n)
			}
			if keys := store.Keys("thumbs"); len(keys) != 0 {
				t.Errorf("objects %v, want none", keys)
			}
			th, err := models.GetThumbnailByResourceID(ctx, db, h2)
			if err != nil {
				t.Fatal(err)
			}
			if th != nil {
				t.Errorf("thumbnail row %+v, want none", th)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := startPrivateTorrent(t, img)
			s3, store := thumbtest.Start(t)
			s := thumbtest.Service(t, s3, pg, f.api(t))
			_, err := s.Generate(ctx, &api.Claims{Uid: c.uid}, c.hash)
			c.check(t, f, store, err)
		})
	}
}
