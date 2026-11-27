package thumbnail_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

const (
	eraseU  = "9c5db5752b7c0123456789abcdef0123456789ab" // an upload
	eraseT  = "0123456789abcdef0123456789abcdef01234567" // a torrent
	eraseH1 = "1111111111111111111111111111111111111111"
	eraseH2 = "2222222222222222222222222222222222222222"
	foreign = "poster/other-x/240.jpg" // a render of another source
)

func thumbRow(t *testing.T, db *pg.DB, id, path string, offset int, kind models.ThumbnailSourceKind, hash string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO thumbnail (resource_id, path, offset_sec, source_kind, hash, format, size) VALUES (?, ?, ?, ?, ?, 'jpg', 1)`,
		id, path, offset, kind, hash); err != nil {
		t.Fatal(err)
	}
}

func rowsOf(t *testing.T, db *pg.DB, id string) []string {
	t.Helper()
	var hashes []string
	if _, err := db.Query(&hashes, `SELECT hash FROM thumbnail WHERE resource_id = ? ORDER BY hash`, id); err != nil {
		t.Fatal(err)
	}
	return hashes
}

func keysEqual(t *testing.T, store *thumbtest.Store, bucket string, want ...string) {
	t.Helper()
	if got := store.Keys(bucket); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("%s: %v, want %v", bucket, got, want)
	}
}

// eraseFixture is one cover file in an upload and in a torrent, H2, and a
// frame of the upload, H1, with their renders and another source's render.
func eraseFixture(t *testing.T, db *pg.DB, store *thumbtest.Store) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM thumbnail`); err != nil {
		t.Fatal(err)
	}
	thumbRow(t, db, eraseU, "/a.mkv", 300, models.ThumbnailSourceFFmpegFrame, eraseH1)
	thumbRow(t, db, eraseU, "/cover.jpg", 0, models.ThumbnailSourceImageFile, eraseH2)
	thumbRow(t, db, eraseT, "/cover.jpg", 0, models.ThumbnailSourceImageFile, eraseH2)
	for _, k := range []string{eraseH1 + ".jpg", eraseH2 + ".jpg"} {
		store.Put("thumbs", k, []byte("x"))
	}
	for _, k := range []string{"poster/thumb-" + eraseH1 + "/240.jpg", "poster/thumb-" + eraseH1 + "/blur-og.jpg", "poster/thumb-" + eraseH2 + "/240.jpg", foreign} {
		store.Put("renders", k, []byte("x"))
	}
}

// restAPI is a rest-api that knows nothing: Erase asks it nothing.
func restAPI(t *testing.T) *api.Api {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	return (&privateTorrent{srv: srv}).api(t)
}

func TestErase(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	a := restAPI(t)
	s3, store := thumbtest.Start(t)
	s := thumbtest.Service(t, s3, pgCS, a)
	eraseFixture(t, db, store)

	if err := s.Erase(ctx, eraseU); err != nil {
		t.Fatalf("erase the upload: %v", err)
	}
	if got := rowsOf(t, db, eraseU); len(got) != 0 {
		t.Errorf("upload rows %v, want none", got)
	}
	if got := rowsOf(t, db, eraseT); !reflect.DeepEqual(got, []string{eraseH2}) {
		t.Errorf("torrent rows %v, want its cover", got)
	}
	keysEqual(t, store, "thumbs", eraseH2+".jpg")
	keysEqual(t, store, "renders", foreign, "poster/thumb-"+eraseH2+"/240.jpg")

	deletes := store.Hits(http.MethodDelete)
	if err := s.Erase(ctx, eraseU); err != nil {
		t.Fatalf("erase again: %v", err)
	}
	keysEqual(t, store, "thumbs", eraseH2+".jpg")
	keysEqual(t, store, "renders", foreign, "poster/thumb-"+eraseH2+"/240.jpg")
	if n := store.Hits(http.MethodDelete) - deletes; n != 0 {
		t.Errorf("%d more DELETEs", n)
	}

	if err := s.Erase(ctx, eraseT); err != nil {
		t.Fatalf("erase the torrent: %v", err)
	}
	keysEqual(t, store, "thumbs")
	keysEqual(t, store, "renders", foreign)

	t.Run("storage fails", func(t *testing.T) {
		s3, store := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pgCS, a)
		eraseFixture(t, db, store)
		store.Fail(http.MethodDelete)
		if err := s.Erase(ctx, eraseU); err == nil {
			t.Fatal("no error")
		}
		if got := rowsOf(t, db, eraseU); len(got) == 0 || got[0] != eraseH1 {
			t.Errorf("upload rows %v, want the frame's kept", got)
		}
		store.Fail("")
		if err := s.Erase(ctx, eraseU); err != nil {
			t.Fatalf("retry: %v", err)
		}
		if got := rowsOf(t, db, eraseU); len(got) != 0 {
			t.Errorf("upload rows %v, want none", got)
		}
		keysEqual(t, store, "thumbs", eraseH2+".jpg")
	})

	t.Run("no service", func(t *testing.T) {
		if err := (*thumbnail.Service)(nil).Erase(ctx, eraseU); err != nil {
			t.Errorf("%v, want nil", err)
		}
	})

	t.Run("no render cache", func(t *testing.T) {
		s3, store := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pgCS, a, "--aws-poster-cache-bucket=")
		eraseFixture(t, db, store)
		if err := s.Erase(ctx, eraseU); err != nil {
			t.Fatal(err)
		}
		keysEqual(t, store, "thumbs", eraseH2+".jpg")
		if n := store.Hits(http.MethodGet); n != 0 {
			t.Errorf("%d list requests, want none", n)
		}
	})
}

// A store of the same bytes for another resource holds the content's lock
// from before its object is put until its row is in: Erase waits for it and
// then sees the row.
func TestEraseWaitsForAStoreOfTheSameBytes(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	img := jpegOf(t)
	sum := sha1.Sum(img)
	h2 := hex.EncodeToString(sum[:])

	t.Run("a held lock", func(t *testing.T) {
		s3, store := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pgCS, restAPI(t))
		if _, err := db.Exec(`DELETE FROM thumbnail`); err != nil {
			t.Fatal(err)
		}
		thumbRow(t, db, eraseU, "/cover.jpg", 0, models.ThumbnailSourceImageFile, h2)
		store.Put("thumbs", h2+".jpg", img)

		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		// A failed step must not keep the lock from the next subtest.
		t.Cleanup(func() { _ = tx.Rollback() })
		if err := models.AdvisoryXactLock(ctx, tx, h2); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- s.Erase(ctx, eraseU) }()
		time.Sleep(200 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("erase returned before the store ended: %v", err)
		default:
		}
		if _, err := tx.Exec(`INSERT INTO thumbnail (resource_id, path, offset_sec, source_kind, hash, format, size) VALUES (?, '/cover.jpg', 0, 1, ?, 'jpg', 1)`, eraseT, h2); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := rowsOf(t, db, eraseT); len(got) != 1 {
			t.Errorf("torrent rows %v, want one", got)
		}
		keysEqual(t, store, "thumbs", h2+".jpg")
	})

	t.Run("the store is Generate's own", func(t *testing.T) {
		f := startDeletable(t, img, func(int, bool) int { return http.StatusOK }, nil)
		s3, store := thumbtest.Start(t)
		s := thumbtest.Service(t, s3, pgCS, f.api(t))
		if _, err := db.Exec(`DELETE FROM thumbnail`); err != nil {
			t.Fatal(err)
		}
		thumbRow(t, db, eraseU, "/cover.jpg", 0, models.ThumbnailSourceImageFile, h2)
		store.Put("thumbs", h2+".jpg", img)
		store.Hold(http.MethodPut, 300*time.Millisecond)

		var genEnd, eraseEnd time.Time
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.Generate(ctx, &api.Claims{Uid: uidA}, eraseT); err != nil {
				t.Errorf("generate: %v", err)
			}
			genEnd = time.Now()
		}()
		time.Sleep(100 * time.Millisecond)
		go func() {
			defer wg.Done()
			if err := s.Erase(ctx, eraseU); err != nil {
				t.Errorf("erase: %v", err)
			}
			eraseEnd = time.Now()
		}()
		wg.Wait()
		if eraseEnd.Before(genEnd) {
			t.Errorf("erase returned %v before generate", genEnd.Sub(eraseEnd))
		}
		if got := rowsOf(t, db, eraseT); len(got) != 1 {
			t.Errorf("torrent rows %v, want one", got)
		}
		keysEqual(t, store, "thumbs", h2+".jpg")
	})
}

// deletable stands in for rest-api and torrent-http-proxy around one
// resource of a single image, poster.jpg. Its lookup answers what answer
// says for the n-th lookup, and whether the image was exported by then;
// gone, when set, runs before every lookup after the export.
type deletable struct {
	srv      *httptest.Server
	mu       sync.Mutex
	exported bool
	lookups  int
}

func startDeletable(t *testing.T, img []byte, answer func(n int, exported bool) int, gone func()) *deletable {
	t.Helper()
	f := &deletable{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/resource/")
		switch {
		case r.URL.Path == "/blob/poster.jpg":
			_, _ = w.Write(img)
		case !ok:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		case strings.HasSuffix(rest, "/list"):
			_, _ = fmt.Fprintf(w, `{"items":[{"id":"i1","name":"poster.jpg","path":"/poster.jpg","type":"file","media_format":"image","size":%d}],"items_count":1}`, len(img))
		case strings.HasSuffix(rest, "/export/i1"):
			f.mu.Lock()
			f.exported = true
			f.mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"exports":{"download":{"url":%q}}}`, f.srv.URL+"/blob/poster.jpg")
		default:
			f.mu.Lock()
			f.lookups++
			n, exported := f.lookups, f.exported
			f.mu.Unlock()
			if exported && gone != nil {
				gone()
			}
			switch status := answer(n, exported); status {
			case http.StatusOK:
				_, _ = fmt.Fprintf(w, `{"id":%q,"private":true}`, rest)
			default:
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"x"}`)
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *deletable) api(t *testing.T) *api.Api {
	return (&privateTorrent{srv: f.srv}).api(t)
}

func (f *deletable) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookups
}

// An upload deleted while its frame was being made: the frame, written after
// the deletion, is taken back. Only an upload's, and only one this call wrote.
func TestGenerateOfADeletedUpload(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	img := jpegOf(t)
	afterExport := func(status int) func(int, bool) int {
		return func(_ int, exported bool) int {
			if exported {
				return status
			}
			return http.StatusOK
		}
	}
	for _, c := range []struct {
		name     string
		id       string
		answer   func(int, bool) int
		purge    bool // the lookup after the export finds the row purged
		existing bool // the resource has its row and object already
		kept     bool
		lookups  int
	}{
		{"deleted meanwhile", eraseU, afterExport(http.StatusNotFound), false, false, false, 1},
		{"lookup fails", eraseU, afterExport(http.StatusInternalServerError), false, false, false, 1},
		{"purged meanwhile", eraseU, afterExport(http.StatusNotFound), true, false, false, 1},
		{"existing row, lookup fails", eraseU, func(int, bool) int { return http.StatusInternalServerError }, false, true, true, 0},
		{"still there", eraseU, afterExport(http.StatusOK), false, false, true, 1},
		{"torrent", eraseT, afterExport(http.StatusNotFound), false, false, true, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := db.Exec(`DELETE FROM thumbnail`); err != nil {
				t.Fatal(err)
			}
			var gone func()
			if c.purge {
				gone = func() {
					if _, err := db.Exec(`DELETE FROM thumbnail WHERE resource_id = ?`, c.id); err != nil {
						t.Error(err)
					}
				}
			}
			f := startDeletable(t, img, c.answer, gone)
			s3, store := thumbtest.Start(t)
			s := thumbtest.Service(t, s3, pgCS, f.api(t))
			if c.existing {
				thumbRow(t, db, c.id, "/poster.jpg", 0, models.ThumbnailSourceImageFile, eraseH1)
				store.Put("thumbs", eraseH1+".jpg", img)
			}
			th, err := s.Generate(ctx, &api.Claims{Uid: uidA}, c.id)
			rows := rowsOf(t, db, c.id)
			if c.kept {
				if err != nil || th == nil {
					t.Errorf("%v, %v; want the thumbnail", th, err)
				}
				if len(rows) != 1 || len(store.Keys("thumbs")) != 1 {
					t.Errorf("rows %v, objects %v; want the thumbnail's", rows, store.Keys("thumbs"))
				}
			} else {
				if !errors.Is(err, thumbnail.ErrNoSource) {
					t.Errorf("%v, want ErrNoSource", err)
				}
				if len(rows) != 0 || len(store.Keys("thumbs")) != 0 {
					t.Errorf("rows %v, objects %v; want none", rows, store.Keys("thumbs"))
				}
			}
			if n := f.count(); n != c.lookups {
				t.Errorf("%d lookups, want %d", n, c.lookups)
			}
		})
	}
}
