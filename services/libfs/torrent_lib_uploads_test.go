package libfs

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vfs"
	"github.com/webtor-io/web-ui/services/web"
)

// WebDAV torrents/ is the torrents section: an upload is neither listed nor
// found by name there, so a DELETE of its .torrent cannot take its library
// row away.
func TestWebDAVTorrentsHidesUploads(t *testing.T) {
	const (
		hash = "0123456789abcdef0123456789abcdef01234567"
		up   = "9c5db5752b7c0123456789abcdef0123456789ab"
	)
	db := pgtest.Start(t)
	var uid uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&uid), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	for id, name := range map[string]string{hash: "Film", up: "Photos"} {
		if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, ?, 1, 1)`, id, name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO library (user_id, resource_id, name) VALUES (?, ?, ?)`, uid, id, name); err != nil {
			t.Fatal(err)
		}
	}
	d := &TorrentLibraryDirectory{pg: pgtest.CS(t, db)}
	ctx := context.WithValue(context.Background(), web.Context{}, &web.Context{User: &auth.User{ID: uid}})

	fis, err := d.ReadDir(ctx, "/", false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, fi := range fis {
		names = append(names, fi.Path)
	}
	if !reflect.DeepEqual(names, []string{"Film.torrent"}) {
		t.Errorf("ReadDir: %v, want only Film.torrent", names)
	}

	notFound := func(op string, err error) {
		t.Helper()
		var he *vfs.HTTPError
		if !errors.As(err, &he) || he.Code != 404 {
			t.Errorf("%s: %v, want a 404", op, err)
		}
	}
	_, err = d.Stat(ctx, "/Photos.torrent")
	notFound("Stat", err)
	notFound("RemoveAll", d.RemoveAll(ctx, "/Photos.torrent", nil))
	if l, err := models.GetLibraryByResourceID(context.Background(), db, uid, up); err != nil || l == nil {
		t.Errorf("the upload's library row: %+v, %v; want it kept", l, err)
	}
	// Last: an Open that finds the row goes on to rest-api, which d has not.
	_, _, err = d.Open(ctx, "/Photos.torrent")
	notFound("Open", err)
}
