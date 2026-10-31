// Package pgtest starts a disposable Postgres for tests whose risk lives in
// the SQL actually sent to a real server (a window-function boundary, NULL
// against '{}', what go-pg writes for a nil value), which a stub cannot
// catch.
package pgtest

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
)

// Start brings up a disposable postgres:17-alpine container, runs this
// repo's real migrations against it, and returns a connected *pg.DB
// (db.Options().Addr is where it listens). The container is removed via
// t.Cleanup. Skips if docker is not on PATH.
func Start(t *testing.T) *pg.DB {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping Postgres-backed test")
	}

	const (
		user     = "pgtest"
		password = "pgtest"
		database = "pgtest"
	)
	containerName := fmt.Sprintf("web-ui-pgtest-%d", time.Now().UnixNano())

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
	// -v: the image's data directory is an anonymous volume, which `rm -f`
	// leaves behind (--rm removes it only when the container exits by
	// itself) -- ~50 MB a run, until Docker's disk is full and every
	// container fails at initdb.
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", "-v", containerName).Run()
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
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")

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
