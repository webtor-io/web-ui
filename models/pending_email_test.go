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
	if a.Email != "owner-a@example.com" {
		t.Errorf("user A's identity email was touched by verification: got %q, want unchanged \"owner-a@example.com\"", a.Email)
	}
	if a.NotificationEmail == nil || *a.NotificationEmail != pendingA {
		t.Errorf("user A's address was not promoted to notification_email: got %v, want %q", a.NotificationEmail, pendingA)
	}
	if a.PendingEmail != nil || a.PendingEmailToken != nil || a.PendingEmailExpiresAt != nil {
		t.Errorf("user A's pending columns were not cleared after verification: %+v", a)
	}

	b, err := GetUserByID(ctx, db, userB)
	if err != nil {
		t.Fatalf("GetUserByID(B): %v", err)
	}
	if b.Email != "owner-b@example.com" {
		t.Fatalf("user A's token touched user B's identity email: it is now %q", b.Email)
	}
	if b.NotificationEmail != nil {
		t.Fatalf("user A's token verified user B's pending address: user B's notification_email is now %v", *b.NotificationEmail)
	}
	if b.PendingEmail == nil || *b.PendingEmail != pendingB {
		t.Fatalf("user B's pending address was disturbed by verifying user A's token: got %v, want %q", b.PendingEmail, pendingB)
	}
	if b.PendingEmailToken == nil || *b.PendingEmailToken != tokenB {
		t.Fatalf("user B's pending token was disturbed by verifying user A's token: got %v, want %q", b.PendingEmailToken, tokenB)
	}
}

// TestGetOrCreateUserAdminLookupSurvivesEmailVerification is the regression
// test for the bug this task almost shipped: VerifyPendingEmail originally
// wrote the confirmed address into email itself. In self-hosted, email on
// the admin row is the literal string "admin", and
// services/auth.registerAdminUser calls GetOrCreateUser(ctx, db, "admin",
// nil) on every single request (there is no session-carried user id in that
// path) -- so overwriting it would make that lookup miss on the very next
// request, falling through to GetOrCreateUser's create branch and silently
// spinning up a second, empty-password "admin" row. The original row --
// carrying the real password hash and the just-confirmed address -- would
// become permanently unreachable through that lookup.
//
// This test does not just check VerifyPendingEmail's return value: it
// replays the exact call registerAdminUser makes afterward, and asserts it
// still finds the same row. See the negative control in this task's report
// for proof this test actually catches the regression -- pointing
// VerifyPendingEmail's Set clause back at "email = pending_email" makes
// this test fail by producing a second row, exactly as it did in
// self-hosted before the fix.
func TestGetOrCreateUserAdminLookupSurvivesEmailVerification(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	// createTestUser inserts with an arbitrary email; the admin row is
	// specifically the sentinel "admin" (services/adminauth/pg_repo.go),
	// which is the exact string registerAdminUser looks up.
	adminID := createTestUser(t, db, "admin")

	const token = "admin-verification-token"
	pending := "operator@example.com"
	if err := SetPendingEmail(ctx, db, adminID, pending, token, time.Now().Add(1*time.Hour)); err != nil {
		t.Fatalf("SetPendingEmail: %v", err)
	}

	ok, err := VerifyPendingEmail(ctx, db, token)
	if err != nil {
		t.Fatalf("VerifyPendingEmail: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPendingEmail did not report success")
	}

	// This is the actual call services/auth.registerAdminUser makes on
	// every request in self-hosted.
	found, isNew, err := GetOrCreateUser(ctx, db, "admin", nil)
	if err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if isNew {
		t.Fatal("GetOrCreateUser created a NEW admin row instead of finding the existing one -- the admin lookup broke, exactly as it did before this fix")
	}
	if found.UserID != adminID {
		t.Fatalf("GetOrCreateUser found a different row: got user_id %s, want the original admin row %s", found.UserID, adminID)
	}

	// Belt and suspenders on the same fact, checked a different way: there
	// must be exactly one row with email = 'admin' in the whole table, not
	// two.
	count, err := db.Model((*User)(nil)).Context(ctx).Where("email = ?", "admin").Count()
	if err != nil {
		t.Fatalf("count admin rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("found %d rows with email = 'admin', want exactly 1 -- verifying a pending address duplicated the admin account", count)
	}

	// And the fix itself: identity (email) is untouched, the confirmed
	// address landed in notification_email instead.
	admin, err := GetUserByID(ctx, db, adminID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if admin.Email != "admin" {
		t.Errorf("admin's identity email was changed: got %q, want \"admin\"", admin.Email)
	}
	if admin.NotificationEmail == nil || *admin.NotificationEmail != pending {
		t.Errorf("notification_email was not set to the confirmed address: got %v, want %q", admin.NotificationEmail, pending)
	}
}
