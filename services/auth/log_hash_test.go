package auth

import (
	"testing"

	uuid "github.com/satori/go.uuid"
)

// The value is pinned, not just "stable": docs/stremio.md joins these log
// lines to the account tables with left(md5(user_id::text), 16), and a
// change of algorithm, of the text form hashed or of the length would make
// that join silently empty. Expected value: printf '%s' <uuid> | md5 | cut -c1-16.
func TestLogHashMatchesTheSQLForm(t *testing.T) {
	id := uuid.FromStringOrNil("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	if got, want := LogHash(id), "1677cad08bd5b077"; got != want {
		t.Errorf("LogHash = %q, want %q (= left(md5(user_id::text), 16))", got, want)
	}
}

func TestLogHashOfNoAccountIsEmpty(t *testing.T) {
	if got := LogHash(uuid.Nil); got != "" {
		t.Errorf("LogHash(uuid.Nil) = %q, want \"\" — an anonymous request has no account to name", got)
	}
}
