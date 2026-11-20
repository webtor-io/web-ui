package library

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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/poster_resolver"
	"github.com/webtor-io/web-ui/services/private_torrent"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

// Nothing a private resource's picture depends on -- who asks -- reaches a
// shared cache: the picture, a refusal, a miss and an error all go out with
// "private, no-store" and keep the session cookie. Only the picture of a
// resource rest-api knows and does not call private, and the brand banner,
// are public for a day, on 200 and on 304.
func TestPosterHeaders(t *testing.T) {
	const (
		hp   = "0123456789abcdef0123456789abcdef01234560" // private
		hq   = "0123456789abcdef0123456789abcdef01234561" // public
		hu   = "0123456789abcdef0123456789abcdef01234562" // unknown to rest-api, matched to a title
		hv   = "0123456789abcdef0123456789abcdef01234563" // unknown to rest-api, nothing about it here
		uidA = "76543210fedcba9876543210fedcba9876543210" // holds a record of hp
		uidB = "89abcdef0123456789abcdef0123456789abcdef" // holds none
	)
	t.Chdir("../..") // the banner is read from ./pub
	db := pgtest.Start(t)
	pg := pgtest.CS(t, db)
	s3, store := thumbtest.Start(t)
	private_torrent.SetEnforce(true)
	t.Cleanup(func() { private_torrent.SetEnforce(false) })
	ctx := context.Background()

	for i, h := range []string{hp, hq, hu} {
		img := image.NewGray(image.Rect(0, 0, 8, 8))
		for j := range img.Pix {
			img.Pix[j] = uint8(i * 64)
		}
		var body bytes.Buffer
		if err := jpeg.Encode(&body, img, nil); err != nil {
			t.Fatal(err)
		}
		sum := sha1.Sum(body.Bytes())
		sha := hex.EncodeToString(sum[:])
		if _, err := db.Model(&models.MediaInfo{ResourceID: h}).Insert(); err != nil {
			t.Fatal(err)
		}
		if err := models.UpsertThumbnail(ctx, db, &models.Thumbnail{ResourceID: h, Path: "/poster.jpg",
			SourceKind: models.ThumbnailSourceImageFile, Hash: sha, Format: "jpg", Size: int64(body.Len())}); err != nil {
			t.Fatal(err)
		}
		store.Put("thumbs", sha+".jpg", body.Bytes())
	}

	// The artwork of the title hu is matched to.
	var poster bytes.Buffer
	if err := jpeg.Encode(&poster, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(poster.Bytes())
	}))
	t.Cleanup(art.Close)
	m := &models.Movie{VideoContent: &models.VideoContent{ResourceID: hu, Title: "Title"}}
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

	// rest-api: web-ui's own reads and the holder get hp; anyone gets hq; hu
	// and hv it does not have.
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/resource/")
		var (
			internal bool
			uid      string
		)
		if p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (any, error) { return []byte("test"), nil }); err == nil {
			mc := p.Claims.(jwt.MapClaims)
			internal = mc["internal"] == true
			uid, _ = mc["uid"].(string)
		}
		switch {
		case id == hu || id == hv:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		case id == hq || internal || uid == uidA:
			_, _ = fmt.Fprintf(w, `{"id":%q,"private":%t}`, id, id == hp)
		case uid == "":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"x","code":"private_torrent","reason":"sign_in"}`)
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
		}
	}))
	t.Cleanup(rest.Close)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"rest-api down"}`)
	}))
	t.Cleanup(down.Close)

	handler := func(srv *httptest.Server, cl *http.Client) *Handler {
		u, _ := url.Parse(srv.URL)
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		for _, f := range api.RegisterFlags(nil) {
			f.Apply(fs)
		}
		if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
			"--webtor-secret=test", "--rapidapi-host="}); err != nil {
			t.Fatal(err)
		}
		a := api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
		th := thumbtest.Service(t, s3, pg, a)
		return &Handler{posterResolver: poster_resolver.New(s3, pg, cl, th, a, "")}
	}
	h := handler(rest, nil)
	withArt := handler(rest, art.Client())
	restDown := handler(down, nil)

	gin.SetMode(gin.TestMode)
	const private, public = "private, no-store", "public, max-age=86400"
	etags := map[string]string{}
	for _, c := range []struct {
		name   string
		h      *Handler
		path   string
		claims *api.Claims
		signed bool   // a signed-in user
		etagOf string // If-None-Match: the ETag answered to this earlier case
		code   int
		cc     string
		cookie string // "kept", "dropped", or "" when it does not matter
	}{
		{"holder", h, "/lib/poster/" + hp + "/300.jpg", &api.Claims{Uid: uidA}, false, "", http.StatusOK, private, "kept"},
		{"holder, etag", h, "/lib/poster/" + hp + "/300.jpg", &api.Claims{Uid: uidA}, false, "holder", http.StatusNotModified, private, "kept"},
		{"stranger", h, "/lib/poster/" + hp + "/300.jpg", &api.Claims{Uid: uidB}, false, "", http.StatusNotFound, private, ""},
		{"stranger, etag", h, "/lib/poster/" + hp + "/300.jpg", &api.Claims{Uid: uidB}, false, "holder", http.StatusNotFound, private, ""},
		{"anonymous", h, "/lib/poster/" + hp + "/300.jpg", &api.Claims{}, false, "", http.StatusNotFound, private, ""},
		{"banner", h, "/lib/poster/" + hp + "/og.jpg", &api.Claims{}, false, "", http.StatusOK, public, "dropped"},
		{"public", h, "/lib/poster/" + hq + "/300.jpg", &api.Claims{}, false, "", http.StatusOK, public, "dropped"},
		{"public, etag", h, "/lib/poster/" + hq + "/300.jpg", &api.Claims{}, false, "public", http.StatusNotModified, public, "dropped"},
		{"unknown, artwork", withArt, "/lib/poster/" + hu + "/300.jpg", &api.Claims{}, false, "", http.StatusOK, private, "kept"},
		{"unknown, artwork, etag", withArt, "/lib/poster/" + hu + "/300.jpg", &api.Claims{}, false, "unknown, artwork", http.StatusNotModified, private, "kept"},
		{"unknown, banner", h, "/lib/poster/" + hv + "/og.jpg", &api.Claims{}, false, "", http.StatusOK, public, "dropped"},
		{"bad file", h, "/lib/poster/" + hq + "/x.png", &api.Claims{}, false, "", http.StatusInternalServerError, private, ""},
		{"rest-api down", restDown, "/lib/poster/" + hq + "/300.jpg", &api.Claims{}, false, "", http.StatusInternalServerError, private, ""},
		{"raw, anonymous", h, "/lib/poster/raw/" + hq + "/300.jpg", &api.Claims{}, false, "", http.StatusUnauthorized, private, ""},
		{"raw, stranger", h, "/lib/poster/raw/" + hp + "/300.jpg", &api.Claims{Uid: uidB}, true, "", http.StatusNotFound, private, ""},
		{"raw, holder", h, "/lib/poster/raw/" + hp + "/300.jpg", &api.Claims{Uid: uidA}, true, "", http.StatusOK, private, "kept"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(gc *gin.Context) {
				ctx := context.WithValue(gc.Request.Context(), api.ClaimsContext{}, c.claims)
				if c.signed {
					ctx = context.WithValue(ctx, auth.UserContext{}, &models.User{UserID: uuid.NewV4()})
				}
				gc.Request = gc.Request.WithContext(ctx)
				// As the session middleware sets its cookie before any handler.
				gc.Header("Set-Cookie", "s=1")
				gc.Next()
			})
			r.GET("/lib/poster/:resource_id/:file", c.h.posterResource)
			r.GET("/lib/poster/raw/:resource_id/:file", c.h.posterResourceRaw)
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.etagOf != "" {
				if etags[c.etagOf] == "" {
					t.Fatalf("no ETag from %q", c.etagOf)
				}
				req.Header.Set("If-None-Match", etags[c.etagOf])
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			res := w.Result()

			if res.StatusCode != c.code {
				t.Fatalf("status %d, want %d", res.StatusCode, c.code)
			}
			if c.code == http.StatusOK {
				if ct := http.DetectContentType(w.Body.Bytes()); ct != "image/jpeg" {
					t.Errorf("body is %s, want image/jpeg", ct)
				}
				etags[c.name] = res.Header.Get("ETag")
			}
			if got := res.Header.Get("Cache-Control"); got != c.cc {
				t.Errorf("Cache-Control %q, want %q", got, c.cc)
			}
			switch cookie := res.Header.Get("Set-Cookie"); c.cookie {
			case "kept":
				if cookie == "" {
					t.Error("Set-Cookie dropped from an answer that is not cached")
				}
			case "dropped":
				if cookie != "" {
					t.Errorf("Set-Cookie %q on a public answer: the edge would not cache it", cookie)
				}
			}
		})
	}
}
