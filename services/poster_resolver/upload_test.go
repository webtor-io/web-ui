package poster_resolver

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"testing"

	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

const (
	upload0 = "9c5db5752b7c0123456789abcdef0123456789ab" // a user's upload
	upload1 = "9c5db5752b7c1123456789abcdef0123456789ab" // home-hosted, not a user's upload
)

// An upload's picture has a source while rest-api knows the upload, and none
// at all once rest-api no longer knows it: its enrichment rows outlive its
// files.
// Other IDs rest-api does not know are looked up as before. The service has
// no database, so reaching Resolve fails with "no db"; a failed lookup is an
// error, never "no source".
func TestPosterSourceOfAnUpload(t *testing.T) {
	t.Chdir("../..") // the banner is read from ./pub
	ctx := context.Background()
	noDB := func(t *testing.T, res *Result, err error) { wantFailure(t, res, err, "no db") }
	for _, c := range []struct {
		name  string
		id    string
		all   *reply // rest-api's answer to everyone; nil: the resource, private
		file  string
		check func(t *testing.T, res *Result, err error)
	}{
		{"upload, evicted", upload0, notFoundReply, "300.jpg", wantNotFound},
		{"upload, evicted, og", upload0, notFoundReply, "og.jpg", func(t *testing.T, res *Result, err error) {
			wantJPEG(t, res, err)
			if res.Private || res.Unknown {
				t.Errorf("Private = %v, Unknown = %v; the banner is nobody's", res.Private, res.Unknown)
			}
		}},
		{"upload, alive", upload0, nil, "300.jpg", noDB},
		{"home-hosted, not an upload, unknown", upload1, notFoundReply, "300.jpg", noDB},
		{"torrent, unknown", testHash, notFoundReply, "300.jpg", noDB},
		{"upload, rest-api down", upload0, downReply, "300.jpg", func(t *testing.T, res *Result, err error) {
			wantFailure(t, res, err, "rest-api down")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := startRestAPI(t, true)
			f.all = c.all
			s := New(nil, &cs.PG{}, nil, nil, f.api(t), "")
			res, err := s.Get(ctx, &api.Claims{}, c.id, c.file, true, false)
			c.check(t, res, err)
		})
	}
}

// A live upload's picture goes to its owner and to nobody else, whether or
// not rest-api calls the upload private. Once rest-api no longer knows it, a
// warm entry gives it to nobody.
func TestUploadPosterGoesToItsOwner(t *testing.T) {
	db := pgtest.Start(t)
	pg := pgtest.CS(t, db)
	s3, store := thumbtest.Start(t)
	ctx := context.Background()

	// What enrichment leaves for an upload with an image file of its own.
	body := jpegOf(t, 64)
	sum := sha1.Sum(body)
	sha := hex.EncodeToString(sum[:])
	if _, err := db.Model(&models.MediaInfo{ResourceID: upload0}).Insert(); err != nil {
		t.Fatal(err)
	}
	if err := models.UpsertThumbnail(ctx, db, &models.Thumbnail{ResourceID: upload0, Path: "/poster.jpg",
		SourceKind: models.ThumbnailSourceImageFile, Hash: sha, Format: "jpg", Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	store.Put("thumbs", sha+".jpg", body)

	pod := func(t *testing.T, f *restAPI) *Service {
		a := f.api(t)
		return New(s3, pg, nil, thumbtest.Service(t, s3, pg, a), a, "renders")
	}
	get := func(s *Service, viewer *api.Claims) (*Result, error) {
		return s.Get(ctx, viewer, upload0, "300.jpg", false, false)
	}
	owner := func(t *testing.T, res *Result, err error) {
		t.Helper()
		wantJPEG(t, res, err)
		if !res.Private {
			t.Error("Private = false, want true")
		}
	}

	s := pod(t, startRestAPI(t, true))
	t.Run("owner", func(t *testing.T) {
		res, err := get(s, &api.Claims{Uid: uidA})
		owner(t, res, err)
	})
	t.Run("stranger", func(t *testing.T) {
		res, err := get(s, &api.Claims{Uid: uidB})
		wantNotFound(t, res, err)
	})
	t.Run("anonymous", func(t *testing.T) {
		res, err := get(s, &api.Claims{})
		wantNotFound(t, res, err)
	})

	fl := startRestAPI(t, true)
	fl.flagless = true
	sl := pod(t, fl)
	t.Run("owner, rest-api omits private", func(t *testing.T) {
		res, err := get(sl, &api.Claims{Uid: uidA})
		owner(t, res, err)
	})
	t.Run("stranger, rest-api omits private", func(t *testing.T) {
		res, err := get(sl, &api.Claims{Uid: uidB})
		wantNotFound(t, res, err)
	})
	t.Run("anonymous, rest-api omits private", func(t *testing.T) {
		res, err := get(sl, &api.Claims{})
		wantNotFound(t, res, err)
	})

	t.Run("owner, warm, then gone", func(t *testing.T) {
		f := startRestAPI(t, true)
		s := pod(t, f)
		res, err := get(s, &api.Claims{Uid: uidA})
		owner(t, res, err)
		f.set(func(f *restAPI) { f.all = notFoundReply })
		res, err = get(s, &api.Claims{Uid: uidA})
		wantNotFound(t, res, err)
	})
}
