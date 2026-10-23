package models

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

// tmdb.info against a real server: the upsert's ON CONFLICT clause and the
// JSONB filters are SQL, not Go.
func TestTmdbInfoCreditsBackfillSQL(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()
	upsert := func(id int, typ tm.TmdbType, md map[string]any) {
		t.Helper()
		if _, err := tm.UpsertInfo(ctx, db, id, typ, md); err != nil {
			t.Fatal(err)
		}
	}
	imdbOf := func(id int) any {
		t.Helper()
		var v *string
		if _, err := db.QueryOneContext(ctx, pg.Scan(&v), `SELECT imdb_id FROM tmdb.info WHERE tmdb_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return nil
		}
		return *v
	}

	// A TV re-fetch whose external_ids call failed carries no imdb_id: the
	// stored one stays. A new one still replaces it.
	upsert(1, tm.TmdbTypeSeries, map[string]any{"name": "Series A", "imdb_id": "tt1"})
	upsert(1, tm.TmdbTypeSeries, map[string]any{"name": "Series A", "credits": map[string]any{}})
	if got := imdbOf(1); got != "tt1" {
		t.Fatalf("imdb_id erased by a same-title upsert without one: %v", got)
	}
	upsert(1, tm.TmdbTypeSeries, map[string]any{"name": "Series A", "imdb_id": "tt2", "credits": map[string]any{}})
	if got := imdbOf(1); got != "tt2" {
		t.Fatalf("a new imdb_id did not replace the stored one: %v", got)
	}
	// The number taken over by the other type is another title: the old
	// IMDB id must not stay on it, either way round.
	upsert(5, tm.TmdbTypeMovie, map[string]any{"title": "Movie M", "imdb_id": "ttM"})
	upsert(5, tm.TmdbTypeSeries, map[string]any{"name": "Series S"})
	if got := imdbOf(5); got != nil {
		t.Fatalf("a series took over a film's number and kept its imdb_id %v", got)
	}
	upsert(6, tm.TmdbTypeSeries, map[string]any{"name": "Series S", "imdb_id": "ttS"})
	upsert(6, tm.TmdbTypeMovie, map[string]any{"title": "Movie M", "imdb_id": nil})
	if got := imdbOf(6); got != nil {
		t.Fatalf("a film took over a series' number and kept its imdb_id %v", got)
	}

	for id, md := range map[int]map[string]any{
		2: {"title": "Movie B"},
		3: {"title": "Movie C", "credits": nil},
		4: {"title": "Movie D"},
	} {
		upsert(id, tm.TmdbTypeMovie, md)
	}
	ids := func(before time.Time) []int {
		t.Helper()
		rows, err := tm.ListWithoutCredits(ctx, db, before, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, r := range rows {
			out = append(out, r.TmdbID)
		}
		return out
	}
	later := time.Now().Add(time.Hour)
	// A JSON null under "credits" is a key that is there: of the rows without
	// credits (2, 4, and 5 and 6 from above) 3 is not one.
	if got := ids(later); !reflect.DeepEqual(got, []int{2, 4, 5, 6}) {
		t.Fatalf("without credits = %v, want [2 4 5 6]", got)
	}
	if got := ids(time.Now().Add(-time.Hour)); got != nil {
		t.Fatalf("rows written after the cutoff were returned: %v", got)
	}
	if err := tm.MarkNoCredits(ctx, db, 2); err != nil {
		t.Fatal(err)
	}
	if got := ids(later); !reflect.DeepEqual(got, []int{4, 5, 6}) {
		t.Fatalf("after marking 2: %v, want [4 5 6]", got)
	}
	got, _ := tm.GetInfoByID(ctx, db, 2)
	if got == nil || got.Metadata["title"] != "Movie B" || got.Metadata["credits"] == nil {
		t.Fatalf("marking lost the row's metadata or set no credits: %+v", got)
	}
}
