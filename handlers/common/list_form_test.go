package common

import "testing"

func TestSplitIDs(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		// The JS builds these by joining a live array, so trailing commas and
		// stray spaces are the normal shape after a row is removed.
		{"a, b ,,c,", []string{"a", "b", "c"}},
	} {
		got := SplitIDs(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("SplitIDs(%q) = %v, want %v", tt.in, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SplitIDs(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

// TestListOrderPriority pins the convention both settings lists depend on:
// the first row must sort first, and rows are read back with
// `ORDER BY priority DESC`.
func TestListOrderPriority(t *testing.T) {
	o := NewListOrder("a,b,c")
	first, ok := o.Priority("a")
	if !ok {
		t.Fatal("the first row must be named by the order")
	}
	last, _ := o.Priority("c")
	if first <= last {
		t.Errorf("priority of the first row = %d, of the last = %d — the first must be higher", first, last)
	}
	if _, ok := o.Priority("missing"); ok {
		t.Error("an id absent from the order must report so, not silently take priority 0")
	}
}
