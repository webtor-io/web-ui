package migrations

import (
	"testing"

	"github.com/go-pg/migrations/v8"
	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models/pgtest"
)

// TestUpOnEmptyDatabase runs what the server runs at startup, the Go
// migrations included, on a database that has none of them yet. A Go
// migration that selects through a live model lists all of the model's
// columns, so a field whose column a later migration adds breaks every fresh
// install. With no rows the migrations never call their clients.
func TestUpOnEmptyDatabase(t *testing.T) {
	server := pgtest.Start(t)
	if _, err := server.Exec("CREATE DATABASE fresh"); err != nil {
		t.Fatal(err)
	}
	o := server.Options()
	db := pg.Connect(&pg.Options{Addr: o.Addr, User: o.User, Password: o.Password, Database: "fresh"})
	t.Cleanup(func() { _ = db.Close() })

	col := migrations.NewCollection()
	PopulateTorrentSizeBytes(col, nil)
	PopulateUserTiers(col, nil)
	if err := col.DiscoverSQLMigrations("."); err != nil {
		t.Fatal(err)
	}
	if _, _, err := col.Run(db, "init"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := col.Run(db, "up"); err != nil {
		t.Fatalf("up on an empty database: %v", err)
	}
}
