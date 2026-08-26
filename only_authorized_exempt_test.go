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

	for _, want := range []string{"/api/v1", "/stremio", "/s3", "/embed", "/donate"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from the exempt list (%s); it authenticates by its own means and would break", want, got)
		}
	}
	if len(onlyAuthorizedExempt()) != 5 {
		t.Errorf("the exempt list has %d entries (%s); every addition widens what an unauthenticated visitor reaches, so it needs a reason here",
			len(onlyAuthorizedExempt()), got)
	}
}
