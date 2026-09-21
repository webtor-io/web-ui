package models

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
)

// The queries are asserted as text: go-pg builds them lazily, and a group of
// ORs that comes out un-parenthesised still compiles, still runs, and returns
// every row of the table.
func renderSQL(t *testing.T, q *orm.Query, kind string) string {
	t.Helper()
	var b []byte
	var err error
	switch kind {
	case "select":
		b, err = orm.NewSelectQuery(q).AppendQuery(orm.NewFormatter(), nil)
	case "delete":
		b, err = orm.NewDeleteQuery(q).AppendQuery(orm.NewFormatter(), nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	ts := regexp.MustCompile(`'\d{4}-\d\d-\d\d [^']+'`)
	return ts.ReplaceAllString(string(b), "'TS'")
}

func TestCacheIndexWindowsSQL(t *testing.T) {
	db := pg.Connect(&pg.Options{Addr: "127.0.0.1:1"}) // never dialled: nothing is executed
	defer db.Close()
	w := CacheWindows{Probe: 12 * time.Hour, Seeder: 7 * 24 * time.Hour}

	sel := renderSQL(t, w.fresh(db.Model((*CacheIndex)(nil)).
		ColumnExpr("DISTINCT resource_id, file_idx").
		Where("resource_id IN (?)", pg.In([]string{"a", "b"})).
		Where("backend_type = ?", StreamingBackendTypeWebtor)), "select")
	wantSel := `SELECT DISTINCT resource_id, file_idx FROM "cache_index" AS "cache_index" WHERE (resource_id IN ('a','b')) AND (backend_type = 'webtor') AND (((source = 'seeder' AND last_seen_at >= 'TS')) OR ((source <> 'seeder' AND last_seen_at >= 'TS')))`
	if sel != wantSel {
		t.Errorf("select:\n got %s\nwant %s", sel, wantSel)
	}

	del := renderSQL(t, w.stale(db.Model((*CacheIndex)(nil))), "delete")
	if !strings.Contains(del, `WHERE (((source = 'seeder' AND last_seen_at < 'TS')) OR ((source <> 'seeder' AND last_seen_at < 'TS')))`) {
		t.Errorf("delete: %s", del)
	}
}

// What the owner asked for on 2026-09-21: "gone from the seeder" must not
// erase what is known of the Vault. The delete from a seeder event names its
// source; the one from a probe does not.
func TestUnmarkCachedScopesBySource(t *testing.T) {
	db := pg.Connect(&pg.Options{Addr: "127.0.0.1:1"})
	defer db.Close()
	build := func(source CacheSource, idx *int) string {
		q := db.Model((*CacheIndex)(nil)).
			Where("resource_id = ?", "h").
			Where("backend_type = ?", StreamingBackendTypeWebtor)
		q = unmarkScope(q, source, idx)
		return renderSQL(t, q, "delete")
	}
	three := 3
	if got := build(CacheSourceSeeder, nil); !strings.HasSuffix(got, `AND (source = 'seeder')`) {
		t.Errorf("seeder, whole torrent: %s", got)
	}
	if got := build(CacheSourceSeeder, &three); !strings.HasSuffix(got, `AND (source = 'seeder') AND (file_idx = 3)`) {
		t.Errorf("seeder, one file: %s", got)
	}
	if got := build("", &three); strings.Contains(got, "(source") || !strings.HasSuffix(got, `AND (file_idx = 3)`) {
		t.Errorf("probe said no: every source goes, one file only: %s", got)
	}
}
