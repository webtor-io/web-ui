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

// The parameters in an encoded hash are attacker/corruption-reachable: they
// come straight out of the database row being checked. argon2.IDKey panics
// for t=0 and p=0 (see golang.org/x/crypto/argon2's deriveKey), and it has
// no upper bound on m at all — a large-enough m tries to allocate gigabytes
// to terabytes of memory. Verify must reject all of these before ever
// calling argon2.IDKey, never by surviving a panic or an allocation.
//
// Each case is run through a recover() so that a still-panicking
// implementation reports as a clear, isolated test failure instead of
// crashing the whole test binary and hiding the results of the other cases.
//
// Note for anyone bisecting history: against the unguarded implementation,
// the "huge memory" subtest does not panic, it allocates — a 4,000,000,000
// KiB request is large enough to threaten the machine running the test. It
// was therefore never run directly via `go test` against the unguarded
// code; that reproduction was done once, out-of-process, under a hard
// watchdog (see the package's fix-round-1 report). It is safe to run here
// unconditionally once the guard below exists, because the guard rejects it
// before argon2.IDKey is ever called — which is exactly what this test
// exists to keep true.
func TestVerifyRejectsPathologicalParameters(t *testing.T) {
	cases := []struct {
		name string
		hash string
	}{
		{"zero time", "$argon2id$v=19$m=65536,t=0,p=4$c2FsdA$aGFzaA"},
		{"zero threads", "$argon2id$v=19$m=65536,t=1,p=0$c2FsdA$aGFzaA"},
		{"zero memory", "$argon2id$v=19$m=0,t=1,p=4$c2FsdA$aGFzaA"},
		{"huge memory", "$argon2id$v=19$m=4000000000,t=1,p=4$c2FsdA$aGFzaA"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Verify panicked on hash %q: %v", c.hash, r)
				}
			}()
			if Verify(c.hash, "any password") {
				t.Errorf("Verify accepted hash with pathological parameters %q", c.hash)
			}
		})
	}
}
