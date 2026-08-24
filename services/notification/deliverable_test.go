package notification

import "testing"

func TestDeliverable(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  bool
	}{
		{"ordinary address", "user@example.com", true},
		{"subdomain", "user@mail.example.co.uk", true},
		{"plus addressing", "user+tag@example.com", true},
		{"empty", "", false},
		// The one that matters: the self-hosted admin account carries this
		// literal string, and every Email == "" guard in the codebase lets
		// it through today.
		{"admin sentinel", "admin", false},
		{"no at sign", "userexample.com", false},
		{"no domain dot", "user@localhost", false},
		{"nothing before the at", "@example.com", false},
		{"nothing after the at", "user@", false},
		{"whitespace only", "   ", false},
		{"leading and trailing space is trimmed", " user@example.com ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Deliverable(tc.email); got != tc.want {
				t.Fatalf("Deliverable(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}
