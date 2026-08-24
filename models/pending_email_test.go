package models

import (
	"context"
	"testing"
	"time"
)

// TestVerifyPendingEmailExpiredLinkDoesNotPromote is task 7's second rule: a
// verification link that has expired must not promote the pending address.
// Real Postgres (startTestPostgres, shared with
// TestPruneNotificationsKeepingNewestPerUserPartition in this package) is
// what makes "> now()" an assertion about the SQL actually sent, not about
// a mock's Go code.
func TestVerifyPendingEmailExpiredLinkDoesNotPromote(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	userID := createTestUser(t, db, "expired-link@example.com")
	const token = "expired-link-token"
	pending := "new-address@example.com"

	if err := SetPendingEmail(ctx, db, userID, pending, token, time.Now().Add(-1*time.Hour)); err != nil {
		t.Fatalf("SetPendingEmail: %v", err)
	}

	ok, err := VerifyPendingEmail(ctx, db, token)
	if err != nil {
		t.Fatalf("VerifyPendingEmail: %v", err)
	}
	if ok {
		t.Fatal("VerifyPendingEmail reported success for an expired token")
	}

	u, err := GetUserByID(ctx, db, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u.Email != "expired-link@example.com" {
		t.Errorf("email was promoted despite an expired token: got %q", u.Email)
	}
	if u.PendingEmail == nil || *u.PendingEmail != pending {
		t.Errorf("pending_email was cleared despite a failed verification: got %v, want %q", u.PendingEmail, pending)
	}
	if u.PendingEmailToken == nil || *u.PendingEmailToken != token {
		t.Errorf("pending_email_token was cleared despite a failed verification: got %v, want %q", u.PendingEmailToken, token)
	}
}

// TestVerifyPendingEmailTokenScopedToOwner is task 7's third and most
// important rule: a token belonging to one user must not verify another
// user's pending address. Two users are each given their own distinct
// pending address and token; verifying user A's token must promote only
// user A's row and leave user B's pending state completely untouched.
//
// This is the guarantee VerifyPendingEmail's doc comment calls out
// specifically: the WHERE clause matches on the token alone, and the
// partial unique index from migration 71 is what makes that sufficient. A
// version that dropped the token equality (matching only on expiry) would
// pass every single-user test but fail this one, because it would promote
// every unexpired pending row in the same UPDATE -- see the report for the
// negative control.
func TestVerifyPendingEmailTokenScopedToOwner(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	userA := createTestUser(t, db, "owner-a@example.com")
	userB := createTestUser(t, db, "owner-b@example.com")

	const tokenA = "token-belongs-to-a"
	const tokenB = "token-belongs-to-b"
	pendingA := "a-new-address@example.com"
	pendingB := "b-new-address@example.com"

	future := time.Now().Add(1 * time.Hour)
	if err := SetPendingEmail(ctx, db, userA, pendingA, tokenA, future); err != nil {
		t.Fatalf("SetPendingEmail(A): %v", err)
	}
	if err := SetPendingEmail(ctx, db, userB, pendingB, tokenB, future); err != nil {
		t.Fatalf("SetPendingEmail(B): %v", err)
	}

	ok, err := VerifyPendingEmail(ctx, db, tokenA)
	if err != nil {
		t.Fatalf("VerifyPendingEmail(tokenA): %v", err)
	}
	if !ok {
		t.Fatal("VerifyPendingEmail did not report success for user A's own token")
	}

	a, err := GetUserByID(ctx, db, userA)
	if err != nil {
		t.Fatalf("GetUserByID(A): %v", err)
	}
	if a.Email != pendingA {
		t.Errorf("user A's address was not promoted: got %q, want %q", a.Email, pendingA)
	}
	if a.PendingEmail != nil || a.PendingEmailToken != nil || a.PendingEmailExpiresAt != nil {
		t.Errorf("user A's pending columns were not cleared after verification: %+v", a)
	}

	b, err := GetUserByID(ctx, db, userB)
	if err != nil {
		t.Fatalf("GetUserByID(B): %v", err)
	}
	if b.Email != "owner-b@example.com" {
		t.Fatalf("user A's token verified user B's address: user B's email is now %q", b.Email)
	}
	if b.PendingEmail == nil || *b.PendingEmail != pendingB {
		t.Fatalf("user B's pending address was disturbed by verifying user A's token: got %v, want %q", b.PendingEmail, pendingB)
	}
	if b.PendingEmailToken == nil || *b.PendingEmailToken != tokenB {
		t.Fatalf("user B's pending token was disturbed by verifying user A's token: got %v, want %q", b.PendingEmailToken, tokenB)
	}
}
