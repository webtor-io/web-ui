package main

import "testing"

// The token route reads the viewer's tier and api claims, and gin runs a
// middleware only for the routes registered after it. Registered earlier,
// every token request would find no claims and answer 500.
func TestUploadsRegisteredAfterClaims(t *testing.T) {
	pos := callPositions(t, "serve.go", "serve")
	up := pos["wup.RegisterHandler"]
	if up == 0 {
		t.Fatal("serve does not call wup.RegisterHandler")
	}
	for _, mw := range []string{"uc.RegisterHandler", "sapi.RegisterHandler"} {
		if p := pos[mw]; p == 0 || p > up {
			t.Errorf("wup.RegisterHandler must come after %s", mw)
		}
	}
}
