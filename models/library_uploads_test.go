package models

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

const (
	testTorrentID = "0123456789abcdef0123456789abcdef01234567"
	testUpload0   = "9c5db5752b7c0123456789abcdef0123456789ab"
	testUpload1   = "9c5db5752b7c1123456789abcdef0123456789ab"
	testUploadF   = "9c5db5752b7cf123456789abcdef0123456789ab"
)

// putLibraryRow writes a torrent_resource row and the user's library row for
// it, with the timestamps given (updated nil is NULL).
func putLibraryRow(t *testing.T, db *pg.DB, uid uuid.UUID, id, name string, created time.Time, updated *time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes, updated_at) VALUES (?, ?, 1, 1, ?)`, id, name, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO library (user_id, resource_id, name, created_at) VALUES (?, ?, ?, ?)`, uid, id, name, created); err != nil {
		t.Fatal(err)
	}
}

func libraryIDs(t *testing.T, ls []*Library, err error) []string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, l := range ls {
		ids = append(ids, l.ResourceID)
	}
	return ids
}

// Uploads have their own section: Torrents and WebDAV torrents/ do not show
// them, the export, the JSON API and WebDAV all/ show everything.
func TestLibraryUploadsSection(t *testing.T) {
	ctx := context.Background()

	t.Run("sections", func(t *testing.T) {
		db := startTestPostgres(t)
		uid := createTestUser(t, db, "uploads-sections@example.com")
		now := time.Now()
		putLibraryRow(t, db, uid, testTorrentID, "Film", now, nil)
		putLibraryRow(t, db, uid, testUpload0, "Photos", now, nil)
		putLibraryRow(t, db, uid, testUpload1, "Clip", now, nil)

		ls, err := GetLibraryTorrentsList(ctx, db, uid, SortTypeName, LibraryTorrents)
		if got := libraryIDs(t, ls, err); !reflect.DeepEqual(got, []string{testTorrentID}) {
			t.Errorf("torrents: %v, want only the torrent", got)
		}
		ls, err = GetLibraryTorrentsList(ctx, db, uid, SortTypeName, LibraryUploads)
		if got := libraryIDs(t, ls, err); !reflect.DeepEqual(got, []string{testUpload1, testUpload0}) {
			t.Errorf("uploads: %v, want both uploads (Clip, Photos)", got)
		}
		ls, err = GetLibraryTorrentsList(ctx, db, uid, SortTypeName, LibraryAll)
		if got := libraryIDs(t, ls, err); !reflect.DeepEqual(got, []string{testUpload1, testTorrentID, testUpload0}) {
			t.Errorf("all: %v, want all three (Clip, Film, Photos)", got)
		}

		torrents, _, _, uploads, err := GetLibraryCounts(ctx, db, uid)
		if err != nil {
			t.Fatal(err)
		}
		if torrents != 1 || uploads != 2 {
			t.Errorf("counts: torrents=%d uploads=%d, want 1 and 2", torrents, uploads)
		}

		if l, err := GetLibraryByName(ctx, db, uid, "Photos"); err != nil || l != nil {
			t.Errorf("by name, an upload: %+v, %v; want nil", l, err)
		}
		if l, err := GetLibraryByName(ctx, db, uid, "Film"); err != nil || l == nil || l.ResourceID != testTorrentID {
			t.Errorf("by name, a torrent: %+v, %v; want its row", l, err)
		}
	})

	t.Run("sorts", func(t *testing.T) {
		db := startTestPostgres(t)
		uid := createTestUser(t, db, "uploads-sorts@example.com")
		now := time.Now()
		ago := func(d time.Duration) *time.Time { at := now.Add(-d); return &at }
		putLibraryRow(t, db, uid, testUpload0, "A", *ago(3 * time.Hour), ago(10*time.Minute))
		putLibraryRow(t, db, uid, testUpload1, "B", *ago(2 * time.Hour), nil)
		putLibraryRow(t, db, uid, testUploadF, "C", *ago(time.Hour), ago(150*time.Minute))
		a, b, c := testUpload0, testUpload1, testUploadF

		for sort, want := range map[SortType][]string{
			SortTypeRecentlyAdded: {c, b, a},
			// An upload never changed counts from when it was added: A 10 min
			// ago, B 2 h, C 150 min.
			SortTypeRecentlyUpdated: {a, b, c},
			SortTypeName:            {a, b, c},
		} {
			ls, err := GetLibraryTorrentsList(ctx, db, uid, sort, LibraryUploads)
			if got := libraryIDs(t, ls, err); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %v, want %v", sort, got, want)
			}
		}
	})

	t.Run("sort numbers", func(t *testing.T) {
		// ?sort=N of a bookmarked page keeps meaning the same sort.
		for st, want := range map[SortType]int{SortTypeName: 1, SortTypeYear: 2, SortTypeRating: 3, SortTypeRecentlyUpdated: 4} {
			if int(st) != want {
				t.Errorf("%s = %d, want %d", st, int(st), want)
			}
		}
		if got := SortTypeRecentlyUpdated.String(); got != "library.sort.recentlyUpdated" {
			t.Errorf("String() = %q", got)
		}
	})
}
