package thumbnail_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/private_torrent"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

// Where private access is only observed, an upload's frame is made — rest-api
// checks its viewer either way — and a private torrent's is not.
func TestGenerateForAnUpload(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Start(t)
	pg := pgtest.CS(t, db)
	img := jpegOf(t)
	sum := sha1.Sum(img)
	key := hex.EncodeToString(sum[:]) + ".jpg"
	was := private_torrent.Enforce()
	private_torrent.SetEnforce(false)
	t.Cleanup(func() { private_torrent.SetEnforce(was) })

	t.Run("upload", func(t *testing.T) {
		f := startPrivateTorrent(t, img, false)
		s3, store := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pg, f.api(t))
		if _, err := s.Generate(ctx, &api.Claims{Uid: uidA}, eraseU); err != nil {
			t.Fatalf("err = %v, want a thumbnail", err)
		}
		th, err := models.GetThumbnailByResourceID(ctx, db, eraseU)
		if err != nil {
			t.Fatal(err)
		}
		if th == nil {
			t.Error("no thumbnail row")
		}
		if keys := store.Keys("thumbs"); len(keys) != 1 || keys[0] != key {
			t.Errorf("objects %v, want [%s]", keys, key)
		}
	})
	t.Run("torrent", func(t *testing.T) {
		f := startPrivateTorrent(t, img, false)
		s3, _ := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pg, f.api(t))
		if _, err := s.Generate(ctx, &api.Claims{Uid: uidA}, testHash); !errors.Is(err, thumbnail.ErrNoSource) {
			t.Errorf("err = %v, want ErrNoSource", err)
		}
		if n := len(f.requests("/list")); n != 0 {
			t.Errorf("%d listing requests, want none", n)
		}
	})
}
