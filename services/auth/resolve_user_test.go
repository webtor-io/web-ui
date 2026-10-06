package auth

import (
	"errors"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/lazymap"

	"github.com/webtor-io/web-ui/models"
)

// fakeStore stands in for SuperTokens + get-or-create (create) and the read
// by primary key (load), counting the calls.
type fakeStore struct {
	rows    map[uuid.UUID]*models.User
	next    *models.User
	err     error
	loadErr error
	creates int
	loads   int
}

func (f *fakeStore) create() (*models.User, bool, error) {
	f.creates++
	if f.err != nil {
		return nil, false, f.err
	}
	return f.next, true, nil
}

func (f *fakeStore) load(id uuid.UUID) (*models.User, error) {
	f.loads++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.rows[id], nil
}

func newIDs() *lazymap.LazyMap[uuid.UUID] {
	return lazymap.New[uuid.UUID](&lazymap.Config{Expire: time.Minute})
}

func TestResolveCached(t *testing.T) {
	alice := &models.User{UserID: uuid.NewV4(), Email: "alice@example.com"}
	bob := &models.User{UserID: uuid.NewV4(), Email: "bob@example.com"}

	t.Run("a miss resolves the long way, a hit reads the row by key", func(t *testing.T) {
		ids, f := newIDs(), &fakeStore{rows: map[uuid.UUID]*models.User{alice.UserID: alice}, next: alice}
		u, isNew, err := resolveCached(ids, "st-alice", f.create, f.load)
		if err != nil || u != alice || !isNew {
			t.Fatalf("miss: %v %v %v", u, isNew, err)
		}
		u, isNew, err = resolveCached(ids, "st-alice", f.create, f.load)
		if err != nil || u != alice || isNew {
			t.Fatalf("hit: %v %v %v", u, isNew, err)
		}
		if f.creates != 1 || f.loads != 1 {
			t.Fatalf("creates=%d loads=%d, want 1 and 1", f.creates, f.loads)
		}
	})

	t.Run("each SuperTokens user keeps its own web-ui user", func(t *testing.T) {
		ids := newIDs()
		rows := map[uuid.UUID]*models.User{alice.UserID: alice, bob.UserID: bob}
		fa, fb := &fakeStore{rows: rows, next: alice}, &fakeStore{rows: rows, next: bob}
		_, _, _ = resolveCached(ids, "st-alice", fa.create, fa.load)
		_, _, _ = resolveCached(ids, "st-bob", fb.create, fb.load)
		if u, _, _ := resolveCached(ids, "st-alice", fb.create, fb.load); u != alice {
			t.Fatalf("st-alice resolved to %v", u)
		}
		if u, _, _ := resolveCached(ids, "st-bob", fa.create, fa.load); u != bob {
			t.Fatalf("st-bob resolved to %v", u)
		}
	})

	t.Run("a deleted row is resolved afresh and the new pair kept", func(t *testing.T) {
		carol := &models.User{UserID: uuid.NewV4(), Email: "alice@example.com"}
		ids, f := newIDs(), &fakeStore{rows: map[uuid.UUID]*models.User{alice.UserID: alice}, next: alice}
		_, _, _ = resolveCached(ids, "st-alice", f.create, f.load)
		// The account is deleted; signing in again creates a new row.
		f.rows, f.next = map[uuid.UUID]*models.User{carol.UserID: carol}, carol
		if u, _, err := resolveCached(ids, "st-alice", f.create, f.load); err != nil || u != carol || f.creates != 2 {
			t.Fatalf("after deletion: %v %v creates=%d", u, err, f.creates)
		}
		if u, _, err := resolveCached(ids, "st-alice", f.create, f.load); err != nil || u != carol || f.creates != 2 {
			t.Fatalf("the new pair was not kept: %v %v creates=%d", u, err, f.creates)
		}
	})

	t.Run("a failure is returned and not kept", func(t *testing.T) {
		ids, f := newIDs(), &fakeStore{rows: map[uuid.UUID]*models.User{alice.UserID: alice}, err: errors.New("core down")}
		if _, _, err := resolveCached(ids, "st-alice", f.create, f.load); err == nil {
			t.Fatal("want the error")
		}
		f.err, f.next = nil, alice
		if u, _, err := resolveCached(ids, "st-alice", f.create, f.load); err != nil || u != alice || f.creates != 2 {
			t.Fatalf("after recovery: %v %v creates=%d", u, err, f.creates)
		}
	})

	// 2026-10-05: Postgres refused connections (57P03) and the read by key
	// failed for every signed-in request. It must stay an error -- (nil, nil)
	// here is an anonymous visitor to myVerifySession.
	t.Run("a failed read by key is an error, not no user", func(t *testing.T) {
		ids, f := newIDs(), &fakeStore{rows: map[uuid.UUID]*models.User{alice.UserID: alice}, next: alice}
		_, _, _ = resolveCached(ids, "st-alice", f.create, f.load)
		f.loadErr = errors.New("57P03 the database system is not yet accepting connections")
		if u, _, err := resolveCached(ids, "st-alice", f.create, f.load); err == nil || u != nil {
			t.Fatalf("got %v %v, want the error", u, err)
		}
	})

	t.Run("no user and no error stays no user, not kept", func(t *testing.T) {
		// createUser's answer when no database is configured.
		ids, f := newIDs(), &fakeStore{}
		if u, _, err := resolveCached(ids, "st-x", f.create, f.load); err != nil || u != nil {
			t.Fatalf("got %v %v", u, err)
		}
		f.next = alice
		if u, _, err := resolveCached(ids, "st-x", f.create, f.load); err != nil || u != alice || f.creates != 2 {
			t.Fatalf("after the database is back: %v %v creates=%d", u, err, f.creates)
		}
	})

	t.Run("a panic is an error and does not wedge the user", func(t *testing.T) {
		ids, f := newIDs(), &fakeStore{rows: map[uuid.UUID]*models.User{alice.UserID: alice}, next: alice}
		boom := func() (*models.User, bool, error) { panic("supertokens client") }
		if u, _, err := resolveCached(ids, "st-alice", boom, f.load); err == nil || u != nil {
			t.Fatalf("got %v %v", u, err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if u, _, err := resolveCached(ids, "st-alice", f.create, f.load); err != nil || u != alice {
				t.Errorf("after the panic: %v %v", u, err)
			}
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("the user is wedged after a panic")
		}
	})
}
