package main

import (
	"strings"
	"testing"
)

// The exempt list is the one place where ONLY_AUTHORIZED can be undone by
// omission, so pin what belongs on it. Most entries are surfaces with their
// own authentication; /donate is the exception and is listed because it is
// meant to be public. Anything not listed is closed, which is the direction a
// mistake should fall.
func TestOnlyAuthorizedExemptListsEverySelfAuthenticatingSurface(t *testing.T) {
	got := strings.Join(onlyAuthorizedExempt(), " ")

	for _, want := range []string{"/api/v1", "/stremio", "/s3", "/embed", "/donate", "/profile/email/verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from the exempt list (%s); it authenticates by its own means and would break", want, got)
		}
	}
	// The verification link opens a subtree of /profile, so make it explicit
	// that the rest of the page did not come with it: an entry of "/profile"
	// here would exempt the whole account surface, which is the mistake this
	// assertion exists to catch.
	for _, mustNot := range onlyAuthorizedExempt() {
		if mustNot == "/profile" {
			t.Error("/profile itself is exempt; only /profile/email/verify may be")
		}
	}
	if len(onlyAuthorizedExempt()) != 6 {
		t.Errorf("the exempt list has %d entries (%s); every addition widens what an unauthenticated visitor reaches, so it needs a reason here",
			len(onlyAuthorizedExempt()), got)
	}
}
