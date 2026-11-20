package models

import (
	"context"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models/pgtest"
)

const gib int64 = 1 << 30

// The upload page keeps the owner's library row and the upload's size and
// file count; the stamp moves only when one of them changes.
func TestAddUploadToLibrary(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()
	u := createUploadTestUser(t, db)
	id := testUploadID

	type resource struct {
		Name      string
		SizeBytes int64
		FileCount int
		UpdatedAt *time.Time
	}
	read := func() resource {
		t.Helper()
		var r resource
		if _, err := db.QueryOne(&r, `SELECT name, size_bytes, file_count, updated_at FROM torrent_resource WHERE resource_id = ?`, id); err != nil {
			t.Fatalf("read torrent_resource: %v", err)
		}
		return r
	}
	add := func(name string, size int64, count int) {
		t.Helper()
		if err := AddUploadToLibrary(ctx, db, u, id, name, size, count); err != nil {
			t.Fatalf("AddUploadToLibrary(%q, %d, %d): %v", name, size, count, err)
		}
	}

	if err := CreateUpload(ctx, db, u, id, "Name"); err != nil {
		t.Fatal(err)
	}
	add("Name", 0, 0)
	if r := read(); r.UpdatedAt != nil {
		t.Errorf("nothing changed: updated_at = %v, want NULL", r.UpdatedAt)
	}

	add("Name", 5*gib, 2)
	r := read()
	if r.UpdatedAt == nil || r.SizeBytes != 5*gib || r.FileCount != 2 {
		t.Fatalf("files came: %+v, want updated_at set, %d bytes, 2 files", r, 5*gib)
	}
	t1 := *r.UpdatedAt

	add("Name", 5*gib, 2)
	if r := read(); r.UpdatedAt == nil || !r.UpdatedAt.Equal(t1) {
		t.Errorf("same size and count: updated_at = %v, want %v", r.UpdatedAt, t1)
	}

	if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, gone_at) VALUES (?, ?, now())`, u, id); err != nil {
		t.Fatal(err)
	}
	add("Name", 5*gib, 2)
	var gone *time.Time
	if _, err := db.QueryOne(pg.Scan(&gone), `SELECT gone_at FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, u, id); err != nil {
		t.Fatal(err)
	}
	if gone != nil {
		t.Errorf("gone_at = %v after the page, want NULL", gone)
	}

	if err := RemoveFromLibrary(ctx, db, u, id); err != nil {
		t.Fatal(err)
	}
	add("Name", 5*gib, 2)
	var libName string
	if _, err := db.QueryOne(pg.Scan(&libName), `SELECT name FROM library WHERE user_id = ? AND resource_id = ?`, u, id); err != nil {
		t.Fatalf("library row after the page: %v", err)
	}
	if libName != "Name" {
		t.Errorf("library name = %q, want Name", libName)
	}

	add("", 5*gib, 2)
	if r := read(); r.Name != "Name" {
		t.Errorf("an empty name: name = %q, want Name kept", r.Name)
	}
	add("Other", 5*gib, 2)
	if r := read(); r.Name != "Other" {
		t.Errorf("a new name: name = %q, want Other", r.Name)
	}
}
