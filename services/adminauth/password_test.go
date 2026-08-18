package adminauth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Errorf("encoding: got %q, want an $argon2id$ string", h)
	}
	if !Verify(h, "correct horse battery") {
		t.Error("Verify rejected the password it was built from")
	}
	if Verify(h, "correct horse batterz") {
		t.Error("Verify accepted a wrong password")
	}
}

// Two hashes of the same password must differ: a per-password salt is what
// stops one leaked hash from unlocking every instance sharing that password.
func TestHashIsSalted(t *testing.T) {
	a, err := Hash("same password here")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := Hash("same password here")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical — salt is missing or fixed")
	}
	if !Verify(a, "same password here") || !Verify(b, "same password here") {
		t.Error("a salted hash failed to verify its own password")
	}
}

func TestHashRejectsShortPasswords(t *testing.T) {
	if _, err := Hash("short7c"); !errors.Is(err, ErrTooShort) {
		t.Errorf("7-char password: got err %v, want ErrTooShort", err)
	}
	if _, err := Hash("exactly8"); err != nil {
		t.Errorf("8-char password should be accepted, got %v", err)
	}
}

// A corrupt or foreign hash string must fail closed rather than panic: the
// value comes from the database and may predate this format.
func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{
		"",
		"not a hash",
		"$argon2id$v=19$m=65536,t=1,p=4$onlysalt",
		"$argon2id$v=19$m=abc,t=1,p=4$c2FsdA$aGFzaA",
		"$bcrypt$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA",
	} {
		if Verify(bad, "any password") {
			t.Errorf("Verify accepted malformed hash %q", bad)
		}
	}
}
