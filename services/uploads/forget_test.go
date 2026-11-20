package uploads

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/go-pg/pg/v10"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/auth"
)

// fakePics records the IDs Erase is called with and answers err; once, err
// only on the first call.
type fakePics struct {
	mu    sync.Mutex
	calls []string
	err   error
	once  bool
}

func (p *fakePics) Erase(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, id)
	err := p.err
	if p.once {
		p.err = nil
	}
	return err
}

func (p *fakePics) called() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

var (
	// derived is what enrichment wrote from an upload's files: the next page
	// view brings it back.
	derived = []string{"media_info", "movie", "ai_enrich.query"}
	// userMade is what the user made on an upload's files: nothing brings it
	// back.
	userMade = []string{"watch_history", "user_subtitle"}
	// standing is what the owner's "this upload is gone" stands on.
	standing = []string{"library", "torrent_resource", "upload_reserve"}
	// allTraces is derived and userMade.
	allTraces = append(append([]string{}, derived...), userMade...)
)

// traces writes upload id of u with its library and resource rows, a reserve
// past its token, the movie found in it with its file path, a catalog query
// naming the file, a watch position and a subtitle bound to the file.
func traces(t *testing.T, db *pg.DB, u *auth.User, id string) {
	t.Helper()
	if err := models.CreateUpload(context.Background(), db, u.ID, id, "n"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?, ?, ?, ` + expired + `)`, []any{u.ID, id, gib}},
		{`INSERT INTO media_info (resource_id, status) VALUES (?, ?)`, []any{id, models.MediaInfoStatusDone}},
		{`INSERT INTO movie (resource_id, title, path) VALUES (?, 'n', '/a.mkv')`, []any{id}},
		{`INSERT INTO ai_enrich.query (parsed_title, content_type, resource_id, hint_path) VALUES (?, 1, ?, '/a.mkv')`, []any{id, id}},
		{`INSERT INTO watch_history (user_id, resource_id, path) VALUES (?, ?, '/a.mkv')`, []any{u.ID, id}},
		{`INSERT INTO user_subtitle (user_id, resource_id, path, hash, original_name, format, size) VALUES (?, ?, '/a.mkv', 'h', 'a.srt', 'srt', 1)`, []any{u.ID, id}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
}

// vaultRow writes the vault.resource row of id.
func vaultRow(t *testing.T, db *pg.DB, id string, funded, vaulted bool) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, vaulted, name) VALUES (?, 1, 1, ?, ?, 'n')`,
		id, funded, vaulted); err != nil {
		t.Fatal(err)
	}
}

// expect reports every table where id has rows when there is false, or none
// when it is true.
func expect(t *testing.T, db *pg.DB, id string, there bool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		var n int
		if _, err := db.QueryOne(pg.Scan(&n), `SELECT count(*) FROM `+table+` WHERE resource_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if (n > 0) != there {
			t.Errorf("%s of %s: %d rows, want rows %v", table, id, n, there)
		}
	}
}

func expectCalls(t *testing.T, p *fakePics, want ...string) {
	t.Helper()
	if got := p.called(); !slices.Equal(got, want) {
		t.Errorf("Erase calls %v, want %v", got, want)
	}
}

func TestForget(t *testing.T) {
	const (
		u0 = "9c5db5752b7c0123456789abcdef0123456789ab"
		u1 = "9c5db5752b7c1123456789abcdef0123456789ab"
		tt = "0123456789abcdef0123456789abcdef01234567"
	)
	db := pgtest.Start(t)
	ctx := context.Background()
	u := newUser(t, db)

	t.Run("unsaved", func(t *testing.T) {
		traces(t, db, u, u0)
		pics := &fakePics{}
		if err := Forget(ctx, db, pics, u0, false); err != nil {
			t.Fatal(err)
		}
		expect(t, db, u0, false, derived...)
		expect(t, db, u0, true, userMade...)
		expect(t, db, u0, true, standing...)
		expectCalls(t, pics, u0)
	})

	id := uid(0x101)
	pics := &fakePics{}
	t.Run("unsaved, certain", func(t *testing.T) {
		traces(t, db, u, id)
		if err := Forget(ctx, db, pics, id, true); err != nil {
			t.Fatal(err)
		}
		expect(t, db, id, false, allTraces...)
		expect(t, db, id, true, standing...)
		expectCalls(t, pics, id)
	})
	t.Run("again", func(t *testing.T) {
		if err := Forget(ctx, db, pics, id, true); err != nil {
			t.Fatal(err)
		}
		expect(t, db, id, true, standing...)
		expectCalls(t, pics, id, id)
	})

	for n, c := range []struct {
		name            string
		funded, vaulted bool
	}{
		{"saved", true, true},
		{"saved, being stored", true, false},
		{"saved, unpaid", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			id := uid(0x110 + n)
			traces(t, db, u, id)
			vaultRow(t, db, id, c.funded, c.vaulted)
			pics := &fakePics{}
			if err := Forget(ctx, db, pics, id, true); err != nil {
				t.Fatal(err)
			}
			expect(t, db, id, true, allTraces...)
			expectCalls(t, pics)
		})
	}

	t.Run("row without a copy", func(t *testing.T) {
		id := uid(0x120)
		traces(t, db, u, id)
		vaultRow(t, db, id, false, false)
		pics := &fakePics{}
		if err := Forget(ctx, db, pics, id, true); err != nil {
			t.Fatal(err)
		}
		expect(t, db, id, false, allTraces...)
		expect(t, db, id, true, append([]string{"vault.resource"}, standing...)...)
		expectCalls(t, pics, id)
	})

	t.Run("vault read fails", func(t *testing.T) {
		id := uid(0x121)
		traces(t, db, u, id)
		if _, err := db.Exec(`ALTER TABLE vault.resource RENAME TO resource_off`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.Exec(`ALTER TABLE vault.resource_off RENAME TO resource`); err != nil {
				t.Error(err)
			}
		})
		pics := &fakePics{}
		if err := Forget(ctx, db, pics, id, true); err == nil {
			t.Error("no error")
		}
		expect(t, db, id, true, allTraces...)
		expectCalls(t, pics)
	})

	for _, c := range []struct{ name, id string }{{"torrent", tt}, {"kind 1", u1}} {
		t.Run(c.name, func(t *testing.T) {
			traces(t, db, u, c.id)
			pics := &fakePics{}
			if err := Forget(ctx, db, pics, c.id, true); err != nil {
				t.Fatal(err)
			}
			expect(t, db, c.id, true, allTraces...)
			expectCalls(t, pics)
		})
	}

	t.Run("pictures fail", func(t *testing.T) {
		id := uid(0x122)
		traces(t, db, u, id)
		pics := &fakePics{err: errors.New("s3 down")}
		if err := Forget(ctx, db, pics, id, true); err == nil {
			t.Error("no error")
		}
		expect(t, db, id, false, allTraces...)
		expectCalls(t, pics, id)
		pics.err = nil
		if err := Forget(ctx, db, pics, id, true); err != nil {
			t.Errorf("retry: %v", err)
		}
		expectCalls(t, pics, id, id)
	})

	t.Run("no pictures", func(t *testing.T) {
		id := uid(0x123)
		traces(t, db, u, id)
		if err := Forget(ctx, db, nil, id, true); err != nil {
			t.Fatal(err)
		}
		expect(t, db, id, false, allTraces...)
	})

	t.Run("no db", func(t *testing.T) {
		id := uid(0x124)
		traces(t, db, u, id)
		pics := &fakePics{}
		if err := Forget(ctx, nil, pics, id, true); err == nil {
			t.Error("no error")
		}
		expect(t, db, id, true, allTraces...)
		expectCalls(t, pics)
	})

	t.Run("the neighbour stays", func(t *testing.T) {
		id, neighbour := uid(0x125), uid(0x126)
		traces(t, db, u, id)
		traces(t, db, u, neighbour)
		if err := Forget(ctx, db, &fakePics{}, id, true); err != nil {
			t.Fatal(err)
		}
		expect(t, db, id, false, allTraces...)
		expect(t, db, neighbour, true, allTraces...)
	})
}

func TestForgetUser(t *testing.T) {
	db := pgtest.Start(t)
	ctx := context.Background()

	t.Run("account", func(t *testing.T) {
		u, x := newUser(t, db), newUser(t, db)
		unsaved, saved, other := uid(0x201), uid(0x202), uid(0x203)
		traces(t, db, u, unsaved)
		traces(t, db, u, saved)
		vaultRow(t, db, saved, true, true)
		traces(t, db, x, other)
		pics := &fakePics{}
		s := &Service{pg: pgtest.CS(t, db), Pictures: pics}
		if err := s.ForgetUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		expect(t, db, unsaved, false, allTraces...)
		expect(t, db, saved, false, allTraces...)
		expect(t, db, other, true, allTraces...)
		expect(t, db, saved, true, "vault.resource")
		got := pics.called()
		slices.Sort(got)
		if want := []string{unsaved, saved}; !slices.Equal(got, want) {
			t.Errorf("Erase calls %v, want %v in any order", got, want)
		}
	})

	t.Run("one fails", func(t *testing.T) {
		u := newUser(t, db)
		ids := []string{uid(0x211), uid(0x212), uid(0x213)}
		for _, id := range ids {
			traces(t, db, u, id)
		}
		vaultRow(t, db, ids[1], true, true)
		// The first call fails, whichever ID it is: the order of the account's
		// uploads is not set.
		pics := &fakePics{err: errors.New("s3 down"), once: true}
		s := &Service{pg: pgtest.CS(t, db), Pictures: pics}
		if err := s.ForgetUser(ctx, u.ID); err == nil {
			t.Error("no error")
		}
		for _, id := range ids {
			expect(t, db, id, false, allTraces...)
		}
		if n := len(pics.called()); n != 3 {
			t.Errorf("%d Erase calls, want 3", n)
		}
	})

	t.Run("nil service", func(t *testing.T) {
		if err := (*Service)(nil).ForgetUser(ctx, newUser(t, db).ID); err != nil {
			t.Error(err)
		}
	})
}
