package models

import (
	"context"
	"fmt"
	"sync"
	"testing"

	uuid "github.com/satori/go.uuid"
)

// TestGetOrCreateUserConcurrentFirstRequests pins the sign-in race seen in
// production: right after sign-in the browser sends two requests with the
// fresh session at once (processAuth clicks return-url while nav.js reloads
// the current URL), both miss the email lookup, both insert, and the loser
// failed on user_email_key -- ~13 error pages a day, half of them on the page
// a brand-new user lands on. Every concurrent caller must get the same row.
func TestGetOrCreateUserConcurrentFirstRequests(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	const rounds, callers = 20, 8
	for r := 0; r < rounds; r++ {
		email := fmt.Sprintf("race-%d@example.com", r)
		var pid *string
		if r%2 == 1 { // odd rounds sign in through Patreon
			id := fmt.Sprintf("%d", 4242+r)
			pid = &id
		}
		ids := make([]uuid.UUID, callers)
		news := make([]bool, callers)
		errs := make([]error, callers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				u, isNew, err := GetOrCreateUser(ctx, db, email, pid)
				if err == nil {
					ids[i], news[i] = u.UserID, isNew
				}
				errs[i] = err
			}(i)
		}
		close(start)
		wg.Wait()

		created := 0
		for i := 0; i < callers; i++ {
			if errs[i] != nil {
				t.Fatalf("round %d caller %d: %v", r, i, errs[i])
			}
			if ids[i] != ids[0] {
				t.Fatalf("round %d: callers got different rows %s and %s", r, ids[0], ids[i])
			}
			if news[i] {
				created++
			}
		}
		if created != 1 {
			t.Fatalf("round %d: %d callers reported a new user, want exactly 1", r, created)
		}
		if pid != nil {
			u, err := GetUserByID(ctx, db, ids[0])
			if err != nil {
				t.Fatal(err)
			}
			if u.PatreonUserID == nil || *u.PatreonUserID != *pid {
				t.Fatalf("round %d: patreon_user_id = %v, want %s", r, u.PatreonUserID, *pid)
			}
		}
	}
}

// TestGetOrCreateUserPatreonEmailTakenByAnotherAccount pins the other way
// sign-in failed on user_email_key: the patron changed the Patreon email to
// an address that already names a different, empty account. Moving it onto
// the Patreon-linked row is impossible, and failing it failed every request
// of the session, locking the patron out of the paid account. They must get
// that account, under its old address, and the other row stays as it was.
func TestGetOrCreateUserPatreonEmailTakenByAnotherAccount(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	pid := "1001"
	paid := createTestUser(t, db, "old@example.com")
	if _, err := db.Exec(`UPDATE "user" SET patreon_user_id = ? WHERE user_id = ?`, pid, paid); err != nil {
		t.Fatal(err)
	}
	other := createTestUser(t, db, "new@example.com")

	u, isNew, err := GetOrCreateUser(ctx, db, "new@example.com", &pid)
	if err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if isNew || u.UserID != paid {
		t.Fatalf("got user %s (new=%v), want the Patreon-linked %s", u.UserID, isNew, paid)
	}
	if u.Email != "old@example.com" {
		t.Errorf("returned email %q, want the stored old@example.com", u.Email)
	}
	o, err := GetUserByID(ctx, db, other)
	if err != nil {
		t.Fatal(err)
	}
	if o.Email != "new@example.com" || o.PatreonUserID != nil {
		t.Errorf("the other account changed: email %q, patreon %v", o.Email, o.PatreonUserID)
	}
}
