package models

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

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

// The second transaction reads the clock right after taking the lock. With
// the lock held by the first, that read is no earlier than the first one's
// last read before its commit. clock_timestamp(), not now(): now() is when
// the transaction began, and the second begins before the first commits.
func TestAdvisoryXactLockSerializes(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()
	run := func(t *testing.T, key2 string) (t1, t2 time.Time) {
		t.Helper()
		locked := make(chan struct{})
		first := make(chan error, 1)
		go func() {
			first <- db.RunInTransaction(ctx, func(tx *pg.Tx) error {
				if err := AdvisoryXactLock(ctx, tx, "k"); err != nil {
					return err
				}
				close(locked)
				time.Sleep(200 * time.Millisecond)
				_, err := tx.QueryOneContext(ctx, pg.Scan(&t1), `SELECT clock_timestamp()`)
				return err
			})
		}()
		select {
		case <-locked:
		case err := <-first:
			t.Fatalf("first transaction: %v", err)
		}
		if err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
			if err := AdvisoryXactLock(ctx, tx, key2); err != nil {
				return err
			}
			_, err := tx.QueryOneContext(ctx, pg.Scan(&t2), `SELECT clock_timestamp()`)
			return err
		}); err != nil {
			t.Fatalf("second transaction: %v", err)
		}
		if err := <-first; err != nil {
			t.Fatalf("first transaction: %v", err)
		}
		return t1, t2
	}
	t.Run("same key waits", func(t *testing.T) {
		if t1, t2 := run(t, "k"); t2.Before(t1) {
			t.Errorf("the second took the lock at %v, before the first released it at %v", t2, t1)
		}
	})
	t.Run("another key does not wait", func(t *testing.T) {
		if t1, t2 := run(t, "k2"); !t2.Before(t1) {
			t.Errorf("the second took its lock at %v, after the first released another at %v", t2, t1)
		}
	})
}

// An upload the cleaner removed, or the owner's page found gone, is marked
// gone the way a quota check would mark it: only while its token can bring
// no files back (over, plus the grace) and only when Vault does not keep it.
func TestMarkUploadGoneLikeACheck(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()
	u, other := createUploadTestUser(t, db), createUploadTestUser(t, db)
	id := func(i int) string { return UploadPrefix + fmt.Sprintf("0%027x", i) }
	marked := func(t *testing.T, user uuid.UUID, id string) bool {
		t.Helper()
		var gone *time.Time
		if _, err := db.QueryOne(pg.Scan(&gone), `SELECT gone_at FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, user, id); err != nil {
			t.Fatal(err)
		}
		return gone != nil
	}
	for i, c := range []struct {
		name       string
		writeUntil string
		pledged    bool
		want       bool
	}{
		{"no token", "NULL", false, true},
		{"token over", "now() - interval '6 minutes'", false, true},
		{"token in its grace", "now() - interval '4 minutes'", false, false},
		{"token alive", "now() + interval '10 minutes'", false, false},
		{"pledged", "NULL", true, false},
	} {
		for j, by := range []string{"evicted", "own"} {
			t.Run(c.name+", "+by, func(t *testing.T) {
				id := id(2*i + j)
				if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, write_until) VALUES (?, ?, `+c.writeUntil+`)`, u, id); err != nil {
					t.Fatal(err)
				}
				if c.pledged {
					for _, q := range []string{
						`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, name) VALUES (?0, 1, 1, 'n')`,
						`INSERT INTO vault.pledge (resource_id, user_id, amount) VALUES (?0, ?1, 1)`,
					} {
						if _, err := db.Exec(q, id, u); err != nil {
							t.Fatal(err)
						}
					}
				}
				var err error
				if by == "evicted" {
					err = MarkUploadEvicted(ctx, db, id)
				} else {
					err = MarkOwnUploadGone(ctx, db, u, id)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := marked(t, u, id); got != c.want {
					t.Errorf("marked %v, want %v", got, c.want)
				}
			})
		}
	}

	// A mark already there keeps its time: the export shows when it went.
	for j, mark := range []func(string) error{
		func(id string) error { return MarkUploadEvicted(ctx, db, id) },
		func(id string) error { return MarkOwnUploadGone(ctx, db, u, id) },
	} {
		id := id(100 + j)
		if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, gone_at) VALUES (?, ?, '2020-01-01Z')`, u, id); err != nil {
			t.Fatal(err)
		}
		if err := mark(id); err != nil {
			t.Fatal(err)
		}
		var moved bool
		if _, err := db.QueryOne(pg.Scan(&moved), `SELECT gone_at <> '2020-01-01Z' FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, u, id); err != nil {
			t.Fatal(err)
		}
		if moved {
			t.Errorf("mark %d moved an earlier gone_at", j)
		}
	}

	// The page knows only its viewer: another account's row of the ID stays.
	const shared = testUploadID
	for _, user := range []uuid.UUID{u, other} {
		if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id) VALUES (?, ?)`, user, shared); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkOwnUploadGone(ctx, db, u, shared); err != nil {
		t.Fatal(err)
	}
	if !marked(t, u, shared) || marked(t, other, shared) {
		t.Errorf("marked the viewer's %v and the other account's %v, want only the viewer's", marked(t, u, shared), marked(t, other, shared))
	}
}
