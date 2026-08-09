package libapi

import (
	"strings"
	"testing"
)

func TestNewUserCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewUserCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("bad shape: %q", code)
		}
		for _, r := range strings.ReplaceAll(code, "-", "") {
			if !strings.ContainsRune(userCodeAlphabet, r) {
				t.Fatalf("character outside the unambiguous alphabet: %q in %q", r, code)
			}
		}
		seen[code] = true
	}
	// Not a collision test — just a sanity check the generator isn't stuck.
	if len(seen) < 190 {
		t.Fatalf("suspiciously many duplicates: %d unique of 200", len(seen))
	}
}

func TestNormalizeUserCode(t *testing.T) {
	for in, want := range map[string]string{
		"f7kq-29xd":    "F7KQ-29XD",
		" F7KQ29XD ":   "F7KQ-29XD",
		"f7kq 29xd":    "F7KQ-29XD",
		"":             "",
		"short":        "",
		"waytoolongcode": "",
	} {
		if got := NormalizeUserCode(in); got != want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeDeviceName(t *testing.T) {
	if got := SanitizeDeviceName("  cli\x00\x1b @ host  "); got != "cli @ host" {
		t.Errorf("control characters survived: %q", got)
	}
	long := strings.Repeat("я", 100)
	if got := SanitizeDeviceName(long); len([]rune(got)) != 64 {
		t.Errorf("length not bounded: %d runes", len([]rune(got)))
	}
}
