package models

import (
	"context"
	"testing"

	"github.com/go-pg/pg/v10"
)

// insertTestMovie inserts a media_info parent (movie.resource_id has an
// FK to it) once per resource, then one movie row with the given path.
func insertTestMovie(t *testing.T, db *pg.DB, resourceID, title string, path *string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO media_info (resource_id, status) VALUES (?, 1)
		ON CONFLICT (resource_id) DO NOTHING
	`, resourceID); err != nil {
		t.Fatalf("insert media_info %s: %v", resourceID, err)
	}
	if _, err := db.Exec(`
		INSERT INTO movie (resource_id, title, path) VALUES (?, ?, ?)
	`, resourceID, title, path); err != nil {
		t.Fatalf("insert movie %s/%s: %v", resourceID, title, err)
	}
}

// A multi-film pack has several movie rows under one resource_id.
// GetMovieWithMetadataByResourceID takes whichever the planner hands
// back first, so the stream job could enrich "Film B" with "Film A"'s
// imdb id and send the wrong OpenSubtitles lookup. The path-aware
// variant has to pick by the file actually being played, and refuse to
// guess when it cannot.
func TestGetMovieWithMetadataByResourceIDAndPath(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	packPathA := "/Pack/Film A (1999).mkv"
	packPathB := "/Pack/Film B (2004).mkv"
	insertTestMovie(t, db, "pack", "Film A", &packPathA)
	insertTestMovie(t, db, "pack", "Film B", &packPathB)

	singlePath := "/Single/Some.Other.Name.mkv"
	insertTestMovie(t, db, "single", "Single Film", &singlePath)

	insertTestMovie(t, db, "singlenull", "Null Path Film", nil)

	t.Run("path match wins in a multi-film pack", func(t *testing.T) {
		m, err := GetMovieWithMetadataByResourceIDAndPath(ctx, db, "pack", packPathB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m == nil || m.Title != "Film B" {
			t.Fatalf("got %+v, want Film B", m)
		}
	})

	t.Run("a lone row is used even when its path does not match", func(t *testing.T) {
		m, err := GetMovieWithMetadataByResourceIDAndPath(ctx, db, "single", "/Single/played-under-another-name.mkv")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m == nil || m.Title != "Single Film" {
			t.Fatalf("got %+v, want Single Film", m)
		}
	})

	t.Run("a lone row with a null path is still used", func(t *testing.T) {
		m, err := GetMovieWithMetadataByResourceIDAndPath(ctx, db, "singlenull", "/whatever.mkv")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m == nil || m.Title != "Null Path Film" {
			t.Fatalf("got %+v, want Null Path Film", m)
		}
	})

	t.Run("ambiguous pack with no path match yields nothing", func(t *testing.T) {
		m, err := GetMovieWithMetadataByResourceIDAndPath(ctx, db, "pack", "/Pack/Film C (2011).mkv")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m != nil {
			t.Fatalf("got %+v, want nil (no hint beats a wrong film)", m)
		}
	})

	t.Run("an unknown resource yields nothing", func(t *testing.T) {
		m, err := GetMovieWithMetadataByResourceIDAndPath(ctx, db, "nosuch", "/x.mkv")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m != nil {
			t.Fatalf("got %+v, want nil", m)
		}
	})
}
