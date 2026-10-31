package models

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-pg/pg/v10"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

// tmdb.localized names: NULL ("stored before names were") and '{}'
// ("fetched, nothing to store") are different answers, and the difference
// lives in go-pg's nil-vs-empty map handling.
func TestTmdbLocalizedNamesSQL(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()
	stored := func(id int) *string {
		t.Helper()
		var v *string
		if _, err := db.QueryOneContext(ctx, pg.Scan(&v), `SELECT names::text FROM tmdb.localized WHERE tmdb_id = ? AND lang = 'ru'`, id); err != nil {
			t.Fatal(err)
		}
		return v
	}
	read := func(id int) map[string]string {
		t.Helper()
		loc, err := tm.GetLocalized(ctx, db, id, "ru")
		if err != nil || loc == nil {
			t.Fatalf("GetLocalized(%d): %+v, %v", id, loc, err)
		}
		return loc.Names
	}
	upsert := func(id int, names map[string]string) {
		t.Helper()
		if _, err := tm.UpsertLocalized(ctx, db, id, "ru", "Название", "Описание", names); err != nil {
			t.Fatal(err)
		}
	}

	// A row written before the column existed.
	if _, err := db.ExecContext(ctx, `INSERT INTO tmdb.localized (tmdb_id, lang, title) VALUES (1, 'ru', 'Название')`); err != nil {
		t.Fatal(err)
	}
	if v := stored(1); v != nil {
		t.Errorf("old row stored %q, want NULL", *v)
	}
	if n := read(1); n != nil {
		t.Errorf("NULL read back as %#v, want nil", n)
	}

	upsert(1, map[string]string{})
	if v := stored(1); v == nil || *v != "{}" {
		t.Errorf("empty names stored %v, want '{}'", v)
	}
	if n := read(1); n == nil || len(n) != 0 {
		t.Errorf("'{}' read back as %#v, want an empty map", n)
	}

	want := map[string]string{"10": "Режиссёр А", "20": "Актёр А"}
	upsert(1, want)
	if n := read(1); !reflect.DeepEqual(n, want) {
		t.Errorf("names read back as %#v", n)
	}

	// nil is NULL on the way in too.
	upsert(2, nil)
	if v := stored(2); v != nil {
		t.Errorf("nil names stored %q, want NULL", *v)
	}
}
