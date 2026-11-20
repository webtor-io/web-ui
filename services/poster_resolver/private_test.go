package poster_resolver

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/private_torrent"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

const (
	testHash = "0123456789abcdef0123456789abcdef01234567"
	uidA     = "76543210fedcba9876543210fedcba9876543210" // holds a record of the torrent
	uidC     = "fedcba9876543210fedcba9876543210fedcba98" // holds another record of the same torrent
	uidB     = "89abcdef0123456789abcdef0123456789abcdef" // holds none
)

// hashN is testHash with its last digit replaced, so that cases do not share
// an in-process entry, rest-api's minute cache or a render.
func hashN(n int) string {
	return testHash[:39] + string("0123456789abcdef"[n])
}

type reply struct {
	status int
	body   string
}

var (
	notFoundReply = &reply{http.StatusNotFound, `{"error":"not found"}`}
	downReply     = &reply{http.StatusInternalServerError, `{"error":"rest-api down"}`}
)

// restAPI stands in for rest-api's GET /resource/<hash>. web-ui's own reads
// and the record holders get the resource; anyone else gets what rest-api
// tells a caller without a record of a private one. all replaces the answer
// to every caller, viewers the answer to callers with a uid. flagless: the
// body says "private":false whoever asks, as a rest-api without the field
// does, while who is refused still follows private.
type restAPI struct {
	srv        *httptest.Server
	mu         sync.Mutex
	private    bool
	flagless   bool
	all        *reply
	viewers    *reply
	viewerHits int // requests made with a caller's claims, not web-ui's own
}

func startRestAPI(t *testing.T, private bool) *restAPI {
	t.Helper()
	f := &restAPI{private: private}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := strings.CutPrefix(r.URL.Path, "/resource/")
		if r.Method != http.MethodGet || !ok {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		var (
			internal bool
			uid      string
		)
		if p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (any, error) { return []byte("test"), nil }); err == nil {
			mc := p.Claims.(jwt.MapClaims)
			internal = mc["internal"] == true
			uid, _ = mc["uid"].(string)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if !internal {
			f.viewerHits++
		}
		a := reply{http.StatusOK, fmt.Sprintf(`{"id":%q,"private":%t}`, id, f.private && !f.flagless)}
		switch {
		case f.all != nil:
			a = *f.all
		case uid != "" && f.viewers != nil:
			a = *f.viewers
		case internal || !f.private || uid == uidA || uid == uidC:
		case uid == "":
			a = reply{http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"sign_in"}`}
		default:
			a = reply{http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`}
		}
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *restAPI) set(fn func(f *restAPI)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *restAPI) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.viewerHits
}

// api is a client of this rest-api with its own, empty minute cache.
func (f *restAPI) api(t *testing.T) *api.Api {
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

func setEnforce(t *testing.T, on bool) {
	private_torrent.SetEnforce(on)
	t.Cleanup(func() { private_torrent.SetEnforce(false) })
}

func wantJPEG(t *testing.T, res *Result, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v, want a picture", err)
	}
	if ct := http.DetectContentType(res.Body); ct != "image/jpeg" {
		t.Fatalf("body is %s, want image/jpeg", ct)
	}
}

func wantNotFound(t *testing.T, res *Result, err error) {
	t.Helper()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (the handler's 404)", err)
	}
	if res != nil {
		t.Errorf("result of %d bytes, want none", len(res.Body))
	}
}

// wantFailure: an error that is not ErrNotFound, and no bytes.
func wantFailure(t *testing.T, res *Result, err error, cause string) {
	t.Helper()
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), cause) {
		t.Errorf("err = %v, want an error with %q, not ErrNotFound", err, cause)
	}
	if res != nil {
		t.Errorf("result of %d bytes, want none", len(res.Body))
	}
}

func jpegOf(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Which source a private resource gets, decided before any database: where
// viewers are not checked it has none, and its share card is the banner for
// everyone. The service has no database, so reaching Resolve fails with "no
// db"; a failed lookup is an error, never the default.
func TestPrivatePosterGate(t *testing.T) {
	t.Chdir("../..") // the banner is read from ./pub
	ctx := context.Background()
	banner := func(t *testing.T, f *restAPI, res *Result, err error) {
		wantJPEG(t, res, err)
		if res.Private {
			t.Error("the banner is marked private")
		}
		if n := f.hits(); n != 0 {
			t.Errorf("rest-api asked %d times with the viewer's claims, want 0", n)
		}
	}
	for _, c := range []struct {
		name    string
		enforce bool
		down    bool
		file    string
		check   func(t *testing.T, f *restAPI, res *Result, err error)
	}{
		{"observe, width", false, false, "300.jpg", func(t *testing.T, _ *restAPI, res *Result, err error) {
			wantNotFound(t, res, err)
		}},
		{"enforce, width", true, false, "300.jpg", func(t *testing.T, _ *restAPI, res *Result, err error) {
			wantFailure(t, res, err, "no db")
		}},
		{"enforce, og", true, false, "og.jpg", banner},
		{"observe, og", false, false, "og.jpg", banner},
		{"rest-api down", true, true, "300.jpg", func(t *testing.T, _ *restAPI, res *Result, err error) {
			wantFailure(t, res, err, "rest-api down")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			setEnforce(t, c.enforce)
			f := startRestAPI(t, true)
			if c.down {
				f.all = downReply
			}
			s := New(nil, &cs.PG{}, nil, nil, f.api(t), "")
			res, err := s.Get(ctx, &api.Claims{}, testHash, c.file, true, false)
			c.check(t, f, res, err)
		})
	}
}

// The viewer gets a private resource's picture when rest-api opens the
// resource to them, as the resource page asks. A refusal and an unknown
// resource are "no picture"; a failure to ask is an error, never a yes.
func TestAdmit(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		viewer *api.Claims
		answer *reply // rest-api's answer to the viewer; nil: as to a caller with or without a record
		want   string // "" admitted, "not found", or the failure's text
	}{
		{"holder", &api.Claims{Uid: uidA}, nil, ""},
		{"stranger", &api.Claims{Uid: uidB}, nil, "not found"},
		{"anonymous", &api.Claims{}, nil, "not found"},
		// A route the api middleware did not run on hands Get no claims at all.
		{"no claims", nil, nil, "not found"},
		{"unknown", &api.Claims{Uid: uidA}, notFoundReply, "not found"},
		{"forbidden", &api.Claims{Uid: uidA}, &reply{http.StatusForbidden, `{"error":"access is forbidden"}`}, "not found"},
		{"500", &api.Claims{Uid: uidA}, downReply, "rest-api down"},
		{"429", &api.Claims{Uid: uidA}, &reply{http.StatusTooManyRequests, `{"error":"slow down"}`}, "status=429"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := startRestAPI(t, true)
			f.viewers = c.answer
			s := New(nil, &cs.PG{}, nil, nil, f.api(t), "")
			err := s.admit(ctx, c.viewer, testHash)
			switch c.want {
			case "":
				if err != nil {
					t.Errorf("err = %v, want the viewer admitted", err)
				}
			case "not found":
				wantNotFound(t, nil, err)
			default:
				wantFailure(t, nil, err, c.want)
			}
		})
	}
}

// A private resource's picture goes only to a viewer rest-api admits: from
// a warm in-process entry, on a forced render and from the render cache of a
// pod that has not seen it. A resource rest-api does not know gets no frame,
// but the artwork of a matched title goes to everyone, marked unknown.
func TestPrivatePosterGoesToItsViewer(t *testing.T) {
	db := pgtest.Start(t)
	pg := pgtest.CS(t, db)
	s3, store := thumbtest.Start(t)
	setEnforce(t, true)
	ctx := context.Background()

	// seed leaves for resource n what enrichment leaves for a torrent with an
	// image file of its own: a media_info row, a thumbnail row and the image
	// in the thumbnail bucket, an image no other resource has.
	seed := func(t *testing.T, n int) (h, sha string) {
		t.Helper()
		h = hashN(n)
		body := jpegOf(t, uint8(n*16))
		sum := sha1.Sum(body)
		sha = hex.EncodeToString(sum[:])
		if _, err := db.Model(&models.MediaInfo{ResourceID: h}).Insert(); err != nil {
			t.Fatal(err)
		}
		if err := models.UpsertThumbnail(ctx, db, &models.Thumbnail{ResourceID: h, Path: "/poster.jpg",
			SourceKind: models.ThumbnailSourceImageFile, Hash: sha, Format: "jpg", Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		store.Put("thumbs", sha+".jpg", body)
		return h, sha
	}
	// pod is a web-ui pod: its own in-process entries and rest-api minute
	// cache; the S3 and the database everyone shares.
	pod := func(t *testing.T, f *restAPI, cl *http.Client) *Service {
		a := f.api(t)
		return New(s3, pg, cl, thumbtest.Service(t, s3, pg, a), a, "renders")
	}
	get := func(s *Service, viewer *api.Claims, h string, force bool) (*Result, error) {
		return s.Get(ctx, viewer, h, "300.jpg", force, false)
	}

	// One resource for the next four cases, in this order: the holder's
	// request leaves a warm entry on the pod and a render in S3.
	hc, shaC := seed(t, 1)
	fc := startRestAPI(t, true)
	sc := pod(t, fc, nil)
	t.Run("holder", func(t *testing.T) {
		res, err := get(sc, &api.Claims{Uid: uidA}, hc, false)
		wantJPEG(t, res, err)
		if !res.Private {
			t.Error("Private = false, want true")
		}
	})
	t.Run("stranger, warm", func(t *testing.T) {
		res, err := get(sc, &api.Claims{Uid: uidB}, hc, false)
		wantNotFound(t, res, err)
	})
	t.Run("anonymous", func(t *testing.T) {
		res, err := get(sc, &api.Claims{}, hc, false)
		wantNotFound(t, res, err)
	})
	t.Run("stranger, cold pod", func(t *testing.T) {
		if key := "poster/thumb-" + shaC + "/300.jpg"; !slices.Contains(store.Keys("renders"), key) {
			t.Fatalf("renders %v, want the holder's %s", store.Keys("renders"), key)
		}
		res, err := get(pod(t, fc, nil), &api.Claims{Uid: uidB}, hc, false)
		wantNotFound(t, res, err)
	})

	t.Run("second holder, warm, then gone", func(t *testing.T) {
		h, _ := seed(t, 2)
		f := startRestAPI(t, true)
		s := pod(t, f, nil)
		res, err := get(s, &api.Claims{Uid: uidA}, h, false)
		wantJPEG(t, res, err)
		f.set(func(f *restAPI) { f.all = notFoundReply })
		res, err = get(s, &api.Claims{Uid: uidC}, h, false)
		wantNotFound(t, res, err)
	})
	t.Run("stranger, force", func(t *testing.T) {
		h, _ := seed(t, 3)
		res, err := get(pod(t, startRestAPI(t, true), nil), &api.Claims{Uid: uidB}, h, true)
		wantNotFound(t, res, err)
	})
	t.Run("viewer check fails", func(t *testing.T) {
		h, _ := seed(t, 4)
		f := startRestAPI(t, true)
		f.viewers = downReply
		res, err := get(pod(t, f, nil), &api.Claims{Uid: uidA}, h, false)
		wantFailure(t, res, err, "rest-api down")
	})
	t.Run("public", func(t *testing.T) {
		h, _ := seed(t, 5)
		f := startRestAPI(t, false)
		res, err := get(pod(t, f, nil), &api.Claims{}, h, false)
		wantJPEG(t, res, err)
		if res.Private || res.Unknown {
			t.Errorf("Private = %v, Unknown = %v, want both false", res.Private, res.Unknown)
		}
		if n := f.hits(); n != 0 {
			t.Errorf("rest-api asked %d times with the viewer's claims, want 0", n)
		}
	})
	t.Run("unknown to rest-api", func(t *testing.T) {
		h, _ := seed(t, 6)
		f := startRestAPI(t, true)
		f.all = notFoundReply
		before := store.Hits(http.MethodGet)
		res, err := get(pod(t, f, nil), &api.Claims{}, h, false)
		wantNotFound(t, res, err)
		if n := store.Hits(http.MethodGet) - before; n != 0 {
			t.Errorf("S3 read %d times, want 0: no frame of a resource rest-api does not know", n)
		}
	})
	t.Run("unknown, artwork", func(t *testing.T) {
		h, _ := seed(t, 7)
		poster := jpegOf(t, 200)
		art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/p.jpg" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(poster)
		}))
		t.Cleanup(art.Close)
		m := &models.Movie{VideoContent: &models.VideoContent{ResourceID: h, Title: "Title"}}
		if _, err := db.Model(m).Insert(); err != nil {
			t.Fatal(err)
		}
		mdID, err := models.UpsertMovieMetadata(ctx, db, &models.VideoMetadata{VideoID: "tt0000002", Title: "Title", PosterURL: art.URL + "/p.jpg"})
		if err != nil {
			t.Fatal(err)
		}
		if err := models.LinkMovieToMetadata(ctx, db, m.MovieID, mdID); err != nil {
			t.Fatal(err)
		}
		f := startRestAPI(t, true)
		f.all = notFoundReply
		res, err := get(pod(t, f, art.Client()), &api.Claims{}, h, false)
		wantJPEG(t, res, err)
		if !res.Unknown || res.Private {
			t.Errorf("Unknown = %v, Private = %v, want true, false", res.Unknown, res.Private)
		}
		if n := f.hits(); n != 0 {
			t.Errorf("rest-api asked %d times with the viewer's claims, want 0", n)
		}
	})
	t.Run("observe", func(t *testing.T) {
		private_torrent.SetEnforce(false)
		defer private_torrent.SetEnforce(true)
		h, _ := seed(t, 8)
		before := store.Hits(http.MethodGet)
		res, err := get(pod(t, startRestAPI(t, true), nil), &api.Claims{Uid: uidA}, h, false)
		wantNotFound(t, res, err)
		if n := store.Hits(http.MethodGet) - before; n != 0 {
			t.Errorf("S3 read %d times, want 0: no source where viewers are not checked", n)
		}
	})
}
