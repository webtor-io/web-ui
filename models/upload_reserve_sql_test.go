package models

import (
	"context"
	"os"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models/pgtest"
)

const testUploadID = "9c5db5752b7c0123456789abcdef0123456789ab"

func createUploadTestUser(t *testing.T, db *pg.DB) uuid.UUID {
	t.Helper()
	return createTestUser(t, db, uuid.NewV4().String()+"@example.com")
}

func TestUploadReserveMigration(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()
	u := createUploadTestUser(t, db)

	if _, err := db.Model(&UploadReserve{UserID: u, ResourceID: testUploadID, ReservedBytes: 3 << 30}).Insert(); err != nil {
		t.Fatalf("insert reserve: %v", err)
	}
	got := &UploadReserve{UserID: u, ResourceID: testUploadID}
	if err := db.Model(got).WherePK().Select(); err != nil {
		t.Fatalf("select reserve: %v", err)
	}
	if got.ReservedBytes != 3<<30 || got.WriteUntil != nil || got.GoneAt != nil {
		t.Errorf("reserve = %d, write_until %v, gone_at %v; want %d, nil, nil",
			got.ReservedBytes, got.WriteUntil, got.GoneAt, int64(3<<30))
	}
	if _, err := db.Model(&UploadReserve{UserID: u, ResourceID: testUploadID, ReservedBytes: 1}).Insert(); err == nil {
		t.Error("a second row for the same user and ID was accepted")
	}

	if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'n', 0, 0)`, testUploadID); err != nil {
		t.Fatalf("insert torrent_resource: %v", err)
	}
	r, err := GetResourceByID(ctx, db, testUploadID)
	if err != nil || r == nil {
		t.Fatalf("GetResourceByID = %v, %v", r, err)
	}
	if r.UpdatedAt != nil {
		t.Errorf("updated_at = %v, want nil", r.UpdatedAt)
	}

	down, err := os.ReadFile("../migrations/77_uploads.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	var table *string
	if _, err := db.QueryOne(pg.Scan(&table), `SELECT to_regclass('public.upload_reserve')::text`); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if table != nil {
		t.Errorf("upload_reserve still exists after down: %s", *table)
	}
	var columns int
	if _, err := db.QueryOne(pg.Scan(&columns), `SELECT count(*) FROM information_schema.columns WHERE table_name = 'torrent_resource' AND column_name = 'updated_at'`); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if columns != 0 {
		t.Errorf("torrent_resource.updated_at still exists after down")
	}
}

// The data export reads the reserve rows through this call: another
// account's rows for the same ID must not appear in it.
func TestListAllUploadReservesReturnsOnlyTheAccountsRows(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()
	u := createUploadTestUser(t, db)
	other := createUploadTestUser(t, db)

	for _, r := range []*UploadReserve{
		{UserID: u, ResourceID: testUploadID, ReservedBytes: 3 << 30},
		{UserID: other, ResourceID: testUploadID, ReservedBytes: 1 << 30},
	} {
		if _, err := db.Model(r).Insert(); err != nil {
			t.Fatalf("insert reserve: %v", err)
		}
	}

	list, err := ListAllUploadReserves(ctx, db, u)
	if err != nil {
		t.Fatalf("ListAllUploadReserves: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d rows, want only the account's own one", len(list))
	}
	if list[0].UserID != u || list[0].ResourceID != testUploadID || list[0].ReservedBytes != 3<<30 {
		t.Errorf("row = %+v, want the account's own 3 GiB row", *list[0])
	}
}
