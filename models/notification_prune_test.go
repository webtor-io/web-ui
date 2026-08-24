package models

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-pg/migrations/v8"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// startTestPostgres brings up a disposable postgres:17-alpine container,
// runs this repo's real migrations against it, and returns a connected
// *pg.DB. The container is removed via t.Cleanup.
//
// PruneNotificationsKeepingNewest's whole risk is a window-function
// partition boundary: whether DELETE ranks rows per user_id or across the
// whole table. That is a property of the SQL actually sent to a real
// server, not of Go code -- every other store test in this codebase (see
// services/notification/notification_test.go's mockStore) stubs the
// interface instead, which cannot catch this class of bug. Hence a real,
// disposable database for this one test. Skips if docker is not on PATH.
func startTestPostgres(t *testing.T) *pg.DB {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping Postgres-backed test")
	}

	const (
		user     = "notiftest"
		password = "notiftest"
		database = "notiftest"
	)
	containerName := fmt.Sprintf("web-ui-notification-prune-test-%d", time.Now().UnixNano())

	runArgs := []string{
		"run", "--rm", "-d",
		"--name", containerName,
		"-e", "POSTGRES_USER=" + user,
		"-e", "POSTGRES_PASSWORD=" + password,
		"-e", "POSTGRES_DB=" + database,
		"-p", "127.0.0.1::5432",
		"postgres:17-alpine",
	}
	if out, err := exec.Command("docker", runArgs...).CombinedOutput(); err != nil {
		t.Fatalf("docker run postgres: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
	})

	// pg_isready is checked from inside the container (over its own unix
	// socket) rather than by dialing a host port: the official postgres
	// image starts, stops and restarts its server once during first-time
	// initdb, and a host-side TCP probe can catch it mid-restart and
	// report false readiness.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("postgres container %s did not become ready in time", containerName)
		}
		cmd := exec.Command("docker", "exec", containerName, "pg_isready", "-U", user, "-d", database)
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	portOut, err := exec.Command("docker", "port", containerName, "5432/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	addr := lastLine(string(portOut))
	if addr == "" {
		t.Fatalf("could not determine published port for %s", containerName)
	}

	db := pg.Connect(&pg.Options{
		Addr:     addr,
		User:     user,
		Password: password,
		Database: database,
	})
	t.Cleanup(func() { _ = db.Close() })

	// The TCP port can accept connections slightly before the server is
	// fully ready to serve queries; retry the actual ping too.
	pingDeadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(pingDeadline) {
		lastErr = db.Ping(context.Background())
		if lastErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("postgres did not become queryable: %v", lastErr)
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve this file's path")
	}
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "migrations")

	col := migrations.NewCollection()
	if err := col.DiscoverSQLMigrations(migrationsDir); err != nil {
		t.Fatalf("discover migrations in %s: %v", migrationsDir, err)
	}
	if _, _, err := col.Run(db, "init"); err != nil {
		t.Fatalf("init migrations table: %v", err)
	}
	if _, _, err := col.Run(db, "up"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return db
}

func lastLine(s string) string {
	line := ""
	for _, l := range splitLines(s) {
		if l != "" {
			line = l
		}
	}
	return line
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

// createTestUser inserts a minimal row into "user" -- notification.user_id
// carries a foreign key to it -- and returns the generated id.
func createTestUser(t *testing.T, db *pg.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, email)
	if err != nil {
		t.Fatalf("insert test user %s: %v", email, err)
	}
	return id
}

// insertTestNotification inserts a notification row with an explicit
// created_at, bypassing the ORM's DEFAULT-on-zero-value handling so the
// test controls ordering directly instead of relying on real-time spacing.
func insertTestNotification(t *testing.T, db *pg.DB, userID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id), `
		INSERT INTO notification (key, title, template, body, user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING notification_id
	`, "test-key", "test", "test.html", "body", userID, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert test notification: %v", err)
	}
	return id
}

// TestPruneNotificationsKeepingNewestPerUserPartition is the point of this
// task's Step 6: PruneNotificationsKeepingNewest must rank rows per user,
// not across the whole table. User A is given 105 notifications, all more
// recent than every one of user B's 3 -- the arrangement under which a
// naive `DELETE ... ORDER BY created_at DESC OFFSET keep` (no PARTITION BY
// user_id) still leaves user A with exactly the newest 100 (so a test
// asserting only that would pass) while deleting user B's entire history,
// because none of user B's rows rank in the global top 100. See this
// task's report for the manual psql run that reproduced exactly that
// failure against the naive query before this test was written.
func TestPruneNotificationsKeepingNewestPerUserPartition(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	userA := createTestUser(t, db, "prune-test-user-a@example.com")
	userB := createTestUser(t, db, "prune-test-user-b@example.com")

	const (
		aTotal = 105
		keep   = 100
	)

	// User A: 105 notifications, all within the last two minutes.
	recentBase := time.Now().Add(-2 * time.Minute)
	aIDs := make([]uuid.UUID, aTotal)
	for i := 0; i < aTotal; i++ {
		aIDs[i] = insertTestNotification(t, db, userA, recentBase.Add(time.Duration(i)*time.Second))
	}
	// aIDs is oldest-to-newest; the newest `keep` are the last `keep` entries.
	wantKeptA := aIDs[aTotal-keep:]

	// User B: 3 notifications, all far older than every one of user A's --
	// this is what makes a global (non-partitioned) cutoff wipe user B out
	// entirely rather than merely truncating it.
	oldBase := time.Now().Add(-72 * time.Hour)
	bIDs := []uuid.UUID{
		insertTestNotification(t, db, userB, oldBase),
		insertTestNotification(t, db, userB, oldBase.Add(1*time.Hour)),
		insertTestNotification(t, db, userB, oldBase.Add(2*time.Hour)),
	}

	if err := PruneNotificationsKeepingNewest(ctx, db, keep); err != nil {
		t.Fatalf("PruneNotificationsKeepingNewest: %v", err)
	}

	// Assertion 1: user A, who exceeded the cap, keeps exactly the newest
	// `keep` entries.
	var remainingA []Notification
	if err := db.Model(&remainingA).Where("user_id = ?", userA).Order("created_at ASC").Select(); err != nil {
		t.Fatalf("select remaining notifications for user A: %v", err)
	}
	if len(remainingA) != keep {
		t.Fatalf("user A: %d notifications remain, want %d", len(remainingA), keep)
	}
	for i, n := range remainingA {
		if n.NotificationID != wantKeptA[i] {
			t.Fatalf("user A: remaining[%d] = %s, want %s (the newest %d must survive, in order)", i, n.NotificationID, wantKeptA[i], keep)
		}
	}

	// Assertion 2: user B, who never approached the cap, is untouched. This
	// is the assertion that actually distinguishes a per-user partition
	// from a global cutoff -- see the manual psql reproduction referenced
	// in the doc comment above, where the naive query passed assertion 1
	// but zeroed this one out.
	var remainingB []Notification
	if err := db.Model(&remainingB).Where("user_id = ?", userB).Order("created_at ASC").Select(); err != nil {
		t.Fatalf("select remaining notifications for user B: %v", err)
	}
	if len(remainingB) != len(bIDs) {
		t.Fatalf("user B: %d notifications remain, want %d (untouched) -- pruning user A's feed must not touch user B's history", len(remainingB), len(bIDs))
	}
	for i, n := range remainingB {
		if n.NotificationID != bIDs[i] {
			t.Fatalf("user B: remaining[%d] = %s, want %s -- user B's history was disturbed by pruning user A's feed", i, n.NotificationID, bIDs[i])
		}
	}
}
