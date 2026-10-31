package models

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/go-pg/pg/v10"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

// NULL ("not derived") and '{}' ("derived, none") are different answers, and
// the difference lives in go-pg's nil-vs-empty handling and in SQL.
func TestMetadataGenresSQL(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()
	stored := func(table, videoID string) []string {
		t.Helper()
		var g []string
		if _, err := db.QueryOneContext(ctx, pg.Scan(pg.Array(&g)), `SELECT genres FROM public.? WHERE video_id = ?`, pg.Ident(table), videoID); err != nil {
			t.Fatal(err)
		}
		return g
	}

	for _, k := range []struct {
		table  string
		ct     ContentType
		upsert func(genres []string, title string)
	}{
		{"movie_metadata", ContentTypeMovie, func(g []string, title string) {
			if _, err := UpsertMovieMetadata(ctx, db, &VideoMetadata{VideoID: "tt1", Title: title, Genres: g}); err != nil {
				t.Fatal(err)
			}
		}},
		{"series_metadata", ContentTypeSeries, func(g []string, title string) {
			if _, err := UpsertSeriesMetadata(ctx, db, &VideoMetadata{VideoID: "tt1", Title: title, Genres: g}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		k.upsert(nil, "omdb")
		if g := stored(k.table, "tt1"); g != nil {
			t.Fatalf("%s: a write without genres stored %#v, want NULL", k.table, g)
		}
		k.upsert([]string{"drama", "war"}, "tmdb")
		if g := stored(k.table, "tt1"); !reflect.DeepEqual(g, []string{"drama", "war"}) {
			t.Fatalf("%s: got %#v", k.table, g)
		}
		k.upsert(nil, "omdb again")
		if g := stored(k.table, "tt1"); !reflect.DeepEqual(g, []string{"drama", "war"}) {
			t.Fatalf("%s: a write without genres erased them: %#v", k.table, g)
		}
		k.upsert([]string{}, "tmdb, none")
		if g := stored(k.table, "tt1"); g == nil || len(g) != 0 {
			t.Fatalf("%s: TMDB listing none stored %#v, want '{}'", k.table, g)
		}

		// The backfill pair: only NULL rows are listed and written.
		for _, id := range []string{"tt2", "tt3"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO public.? (video_id, title) VALUES (?, 'x')`, pg.Ident(k.table), id); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := ListMetadataWithoutGenres(ctx, db, k.ct, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("%s: listed %+v, want tt2 and tt3 only", k.table, rows)
		}
		// tt3 is filled by an enrichment between the read and the write.
		if _, err := db.ExecContext(ctx, `UPDATE public.? SET genres = '{comedy}' WHERE video_id = 'tt3'`, pg.Ident(k.table)); err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			rows[i].Genres = []string{}
			if rows[i].VideoID == "tt2" {
				rows[i].Genres = []string{"action", "adventure"}
			}
		}
		n, err := SetMetadataGenres(ctx, db, k.ct, rows)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s: wrote %d rows, want 1", k.table, n)
		}
		if g := stored(k.table, "tt2"); !reflect.DeepEqual(g, []string{"action", "adventure"}) {
			t.Errorf("%s: tt2 got %#v", k.table, g)
		}
		if g := stored(k.table, "tt3"); !reflect.DeepEqual(g, []string{"comedy"}) {
			t.Errorf("%s: the backfill overwrote a newer value: %#v", k.table, g)
		}
		tt2 := rows[slices.IndexFunc(rows, func(r MetadataGenres) bool { return r.VideoID == "tt2" })]
		tt2.Genres = []string{}
		if _, err := db.ExecContext(ctx, `UPDATE public.? SET genres = NULL WHERE video_id = 'tt2'`, pg.Ident(k.table)); err != nil {
			t.Fatal(err)
		}
		if _, err := SetMetadataGenres(ctx, db, k.ct, []MetadataGenres{tt2}); err != nil {
			t.Fatal(err)
		}
		if g := stored(k.table, "tt2"); g == nil || len(g) != 0 {
			t.Errorf("%s: derived-none stored %#v, want '{}'", k.table, g)
		}
		if rows, err := ListMetadataWithoutGenres(ctx, db, k.ct, 10); err != nil || len(rows) != 0 {
			t.Errorf("%s: still listed after the write: %+v %v", k.table, rows, err)
		}
	}

	// The read side: Genres scans NULL as nil and '{}' as an empty slice.
	md, err := GetMovieMetadataByVideoID(ctx, db, "tt1")
	if err != nil || md.Genres == nil || len(md.Genres) != 0 {
		t.Errorf("'{}' read back as %#v, %v", md, err)
	}

	for id, md := range map[int]map[string]any{
		10: {"name": "S", "imdb_id": "tt10", "genres": []any{map[string]any{"id": 10759, "name": "Action & Adventure"}}},
		11: {"title": "M", "imdb_id": "tt10", "genres": []any{}},
		12: {"title": "No genres key"},
		13: {"title": "Not asked for", "imdb_id": "tt13", "genres": []any{map[string]any{"id": 18}}},
	} {
		typ := tm.TmdbTypeMovie
		if id == 10 {
			typ = tm.TmdbTypeSeries
		}
		if _, err := tm.UpsertInfo(ctx, db, id, typ, md); err != nil {
			t.Fatal(err)
		}
	}
	infos, err := tm.ListGenres(ctx, db, []string{"tt10", "tt404"}, []int{12, 404})
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, in := range infos {
		got = append(got, in.TmdbID)
	}
	if !reflect.DeepEqual(got, []int{10, 11, 12}) {
		t.Fatalf("ListGenres returned %v, want 10, 11, 12 in that order", got)
	}
	if infos[0].Type != tm.TmdbTypeSeries || !reflect.DeepEqual(infos[0].Genres, []any{map[string]any{"id": 10759.0, "name": "Action & Adventure"}}) {
		t.Errorf("row 10: %+v", infos[0])
	}
	if infos[1].Genres == nil || len(infos[1].Genres) != 0 || infos[2].Genres != nil {
		t.Errorf("empty / missing genres: %#v / %#v", infos[1].Genres, infos[2].Genres)
	}
	if infos, err := tm.ListGenres(ctx, db, []string{}, []int{}); err != nil || len(infos) != 0 {
		t.Errorf("empty id lists: %v, %v", infos, err)
	}
}
