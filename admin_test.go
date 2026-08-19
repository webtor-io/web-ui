package main

import (
	"flag"
	"testing"

	"github.com/urfave/cli"
)

// TestSetAdminPasswordRejectsEmptyPassword pins the guard that makes the
// command safe to invoke with no argument: it must fail with a usage error
// before ever touching cs.NewPG (which would otherwise try to dial Postgres
// and dominate the failure with a connection error instead of a clear
// "you forgot the password" message).
func TestSetAdminPasswordRejectsEmptyPassword(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	c := cli.NewContext(nil, fs, nil)

	if err := setAdminPassword(c); err == nil {
		t.Fatal("expected an error for a missing password argument, got nil")
	}
}

// TestConfigureAdminRegistersSetPassword pins that `admin set-password` is
// actually wired up and carries the PG flags it needs to reach the database
// — the thing configure.go could silently drop by forgetting to append
// adminCMD to app.Commands.
func TestConfigureAdminRegistersSetPassword(t *testing.T) {
	adminCMD := makeAdminCMD()

	if adminCMD.Name != "admin" {
		t.Fatalf("expected command name %q, got %q", "admin", adminCMD.Name)
	}
	if len(adminCMD.Subcommands) != 1 {
		t.Fatalf("expected 1 subcommand, got %d", len(adminCMD.Subcommands))
	}
	sub := adminCMD.Subcommands[0]
	if sub.Name != "set-password" {
		t.Fatalf("expected subcommand %q, got %q", "set-password", sub.Name)
	}
	if len(sub.Flags) == 0 {
		t.Fatal("expected set-password to carry the registered PG flags")
	}
}
