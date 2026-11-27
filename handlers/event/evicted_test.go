package event

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
)

// neighbour is another upload of the same user.
const neighbour = "9c5db5752b7c0123456789abcdef0123456789ac"

var (
	// fileTraces are the rows that name an upload's files: what enrichment
	// derived from them and what the user made on them.
	fileTraces = []string{"media_info", "movie", "ai_enrich.query", "watch_history", "user_subtitle"}
	// standing is what "this upload is gone" stands on.
	standing = []string{"library", "torrent_resource"}
)

// traces writes the library and resource rows of id for user u and the rows
// that name its file.
func traces(t *testing.T, db *pg.DB, u uuid.UUID, id string) {
	t.Helper()
	if err := models.CreateUpload(context.Background(), db, u, id, "n"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO media_info (resource_id, status) VALUES (?, ?)`, []any{id, models.MediaInfoStatusDone}},
		{`INSERT INTO movie (resource_id, title, path) VALUES (?, 'n', '/a.mkv')`, []any{id}},
		{`INSERT INTO ai_enrich.query (parsed_title, content_type, resource_id, hint_path) VALUES (?, 1, ?, '/a.mkv')`, []any{id, id}},
		{`INSERT INTO watch_history (user_id, resource_id, path) VALUES (?, ?, '/a.mkv')`, []any{u, id}},
		{`INSERT INTO user_subtitle (user_id, resource_id, path, hash, original_name, format, size) VALUES (?, ?, '/a.mkv', 'h', 'a.srt', 'srt', 1)`, []any{u, id}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
}

// The cleaner's event about a whole directory erases what web-ui derived from
// an unsaved upload's files and what the user made on them, and its pictures.
// The event is acknowledged whatever that erasure meets: only the cache
// index's own failure has it delivered again.
func TestUploadEvicted(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	s3, store := thumbtest.Start(t)
	th := thumbtest.Service(t, s3, pgCS, api.New(flags(t), http.DefaultClient)) // asks rest-api nothing
	hook := logtest.NewGlobal()
	t.Cleanup(hook.Reset)
	var u uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&u), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	const forgetWarn = "failed to forget an evicted upload"
	vaultRow := func(funded, vaulted bool) func(*testing.T, *Handler, *fakeIndex) {
		return func(t *testing.T, _ *Handler, _ *fakeIndex) {
			if _, err := db.Exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, vaulted, name) VALUES (?, 1, 1, ?, ?, 'n')`,
				upload0, funded, vaulted); err != nil {
				t.Fatal(err)
			}
		}
	}

	for i, c := range []struct {
		name         string
		id           string
		msg          string
		setup        func(t *testing.T, h *Handler, ci *fakeIndex)
		fails, warns bool
		tracesKept   bool
		picsKept     *bool // nil: not looked at
	}{
		{"evicted", upload0, `{"resource_id":"` + upload0 + `"}`, func(t *testing.T, _ *Handler, _ *fakeIndex) {
			traces(t, db, u, neighbour)
		}, false, false, false, ptr(false)},
		{"saved", upload0, `{"resource_id":"` + upload0 + `"}`, vaultRow(true, false), false, false, true, ptr(true)},
		{"saved, unpaid", upload0, `{"resource_id":"` + upload0 + `"}`, vaultRow(false, true), false, false, true, ptr(true)},
		{"row without a copy", upload0, `{"resource_id":"` + upload0 + `"}`, vaultRow(false, false), false, false, false, ptr(false)},
		{"one file", upload0, `{"resource_id":"` + upload0 + `","file_idx":3}`, nil, false, false, true, ptr(true)},
		{"torrent", torrent, `{"resource_id":"` + torrent + `"}`, nil, false, false, true, ptr(true)},
		{"vault read fails", upload0, `{"resource_id":"` + upload0 + `"}`, func(t *testing.T, _ *Handler, _ *fakeIndex) {
			if _, err := db.Exec(`ALTER TABLE vault.resource RENAME TO resource_off`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := db.Exec(`ALTER TABLE vault.resource_off RENAME TO resource`); err != nil {
					t.Fatal(err)
				}
			})
		}, false, true, true, ptr(true)},
		{"s3 down", upload0, `{"resource_id":"` + upload0 + `"}`, func(t *testing.T, _ *Handler, _ *fakeIndex) {
			store.Fail("DELETE")
			t.Cleanup(func() { store.Fail("") })
		}, false, true, false, ptr(true)},
		{"index fails", upload0, `{"resource_id":"` + upload0 + `"}`, func(_ *testing.T, _ *Handler, ci *fakeIndex) {
			ci.err = errors.New("index down")
		}, true, false, true, ptr(true)},
		{"no thumbnails", upload0, `{"resource_id":"` + upload0 + `"}`, func(_ *testing.T, h *Handler, _ *fakeIndex) {
			h.thumb = nil
		}, false, false, false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, table := range append(slices.Clone(fileTraces), "thumbnail", "vault.resource") {
				if _, err := db.Exec(`DELETE FROM `+table+` WHERE resource_id = ?`, c.id); err != nil {
					t.Fatal(err)
				}
			}
			traces(t, db, u, c.id)
			hash := fmt.Sprintf("%040x", 0x100+i) // the case's own content
			pictures(t, db, store, c.id, hash)
			h := &Handler{pg: pgCS, thumb: th}
			ci := &fakeIndex{}
			if c.setup != nil {
				c.setup(t, h, ci)
			}
			hook.Reset()

			if err := h.uncached(ci, []byte(c.msg)); (err != nil) != c.fails {
				t.Errorf("err = %v, want an error %v", err, c.fails)
			}
			if len(ci.unmarks) != 1 {
				t.Errorf("index unmarked %v, want once", ci.unmarks)
			}
			warned := slices.ContainsFunc(hook.AllEntries(), func(e *log.Entry) bool { return e.Level == log.WarnLevel && e.Message == forgetWarn })
			if warned != c.warns {
				t.Errorf("warned %v, want %v: %v", warned, c.warns, hook.AllEntries())
			}
			expectRows(t, db, c.id, c.tracesKept, fileTraces...)
			expectRows(t, db, c.id, true, standing...)
			if c.picsKept != nil {
				expectRows(t, db, c.id, *c.picsKept, "thumbnail")
				expectObjects(t, store, hash, *c.picsKept)
			}
			if c.name == "evicted" {
				expectRows(t, db, neighbour, true, fileTraces...)
			}
		})
	}
}

func ptr(b bool) *bool { return &b }

// The cleaner's event about a whole directory is the certain "gone": the
// upload's row in My files dims at once, not at its owner's next token. The
// rows a quota check would not ask about are models'; here only who sends it.
func TestUploadEvictedMarksGone(t *testing.T) {
	db := pgtest.Start(t)
	h := &Handler{pg: pgtest.CS(t, db)}
	var u uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&u), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, id, msg string
		want          bool
	}{
		{"evicted", upload0, `{"resource_id":"` + upload0 + `"}`, true},
		{"one file", upload0, `{"resource_id":"` + upload0 + `","file_idx":3}`, false},
		// No reserve row ever names a torrent; one does here, so that a
		// torrent's eviction, the cleaner's everyday, is seen to ask nothing.
		{"torrent", torrent, `{"resource_id":"` + torrent + `"}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id) VALUES (?, ?)
ON CONFLICT (user_id, resource_id) DO UPDATE SET gone_at = NULL`, u, c.id); err != nil {
				t.Fatal(err)
			}
			if err := h.uncached(&fakeIndex{}, []byte(c.msg)); err != nil {
				t.Fatal(err)
			}
			var gone bool
			if _, err := db.QueryOne(pg.Scan(&gone), `SELECT gone_at IS NOT NULL FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, u, c.id); err != nil {
				t.Fatal(err)
			}
			if gone != c.want {
				t.Errorf("marked gone %v, want %v", gone, c.want)
			}
		})
	}
}
