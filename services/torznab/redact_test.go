package torznab

import (
	"context"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{
			in:   `Get "https://jackett.example.com/torznab?apikey=xp4psuagkxqgy0zdm414&t=caps": dial tcp`,
			want: `Get "https://jackett.example.com/torznab?apikey=***&t=caps": dial tcp`,
		},
		{
			in:   "https://jackett.example.com/dl/rutracker/?jackett_apikey=abc123&path=x",
			want: "https://jackett.example.com/dl/rutracker/?jackett_apikey=***&path=x",
		},
		{
			in:   "https://tracker.example.com/rss?passkey=deadbeef",
			want: "https://tracker.example.com/rss?passkey=***",
		},
		// Nothing to hide must come back untouched, so ordinary messages
		// stay readable.
		{in: "dial tcp 10.0.0.1:443: connect: connection refused", want: "dial tcp 10.0.0.1:443: connect: connection refused"},
		{in: "", want: ""},
	} {
		if got := Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRedactErrorKeepsClassification(t *testing.T) {
	// Redaction must not break IsUnreachable: the handler decides which
	// message the user sees from it, and errors.As has to keep reaching
	// through to the original network error.
	c := NewWithOptions(Options{})
	_, err := c.Caps(context.Background(), Endpoint{URL: "http://127.0.0.1:9117/torznab?apikey=supersecret"})
	if err == nil {
		t.Fatal("Caps() reached a loopback address with the guard enabled")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks the api key: %v", err)
	}
	if !IsUnreachable(err) {
		t.Errorf("IsUnreachable(%v) = false after redaction, want true", err)
	}
}

func TestSearchErrorDoesNotLeakKey(t *testing.T) {
	// The search path is the one that runs on every stream request, and its
	// errors are logged by CompositeStream.
	c := NewWithOptions(Options{})
	_, err := c.Search(context.Background(), Endpoint{URL: "http://127.0.0.1:9117/torznab", APIKey: "supersecret"},
		Query{Type: SearchTypeSearch, Q: "x"})
	if err == nil {
		t.Fatal("Search() reached a loopback address with the guard enabled")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks the api key: %v", err)
	}
}
