package pagination

import (
	"fmt"
	"strings"
	"testing"
)

// render turns a pager into a compact string so expectations read like the UI:
// "<" and ">" are the arrows, "…" a gap, "[n]" the current page.
func render(items []Item) string {
	var b strings.Builder
	for _, it := range items {
		switch {
		case it.Prev:
			b.WriteString("<")
		case it.Next:
			b.WriteString(">")
		case it.Gap:
			b.WriteString("…")
		case !it.Active:
			fmt.Fprintf(&b, "[%d]", it.Page)
		default:
			fmt.Fprintf(&b, "%d", it.Page)
		}
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

// The regression this whole change exists for: a huge listing must not produce
// a link per page.
func TestBuildIsBoundedForHugeListings(t *testing.T) {
	// 3803 files at 25/page — the torrent a crawler walked before both OOM
	// kills. The old implementation returned 155 items.
	for _, page := range []uint{1, 2, 77, 152, 153} {
		got := Build(3803, page, 25)
		if len(got) > MaxItems {
			t.Fatalf("page %d produced %d items, cap is %d", page, len(got), MaxItems)
		}
	}

	// Scale the listing by 1000x; the pager must not grow at all.
	huge := Build(3_803_000, 50_000, 25)
	if len(huge) > MaxItems {
		t.Fatalf("3.8M-item listing produced %d items, cap is %d", len(huge), MaxItems)
	}
}

func TestBuildShapes(t *testing.T) {
	for _, tc := range []struct {
		name            string
		total, pageSize int
		page            uint
		want            string
	}{
		{"single page", 10, 25, 1, "< [1] >"},
		{"exact multiple has no trailing empty page", 50, 25, 1, "< [1] 2 >"},
		{"two pages, on second", 50, 25, 2, "< 1 [2] >"},
		{"start of a long run", 3803, 25, 1, "< [1] 2 3 … 153 >"},
		{"middle of a long run", 3803, 25, 77, "< 1 … 75 76 [77] 78 79 … 153 >"},
		{"end of a long run", 3803, 25, 153, "< 1 … 151 152 [153] >"},
		{"no gap when the elision would cover one page", 175, 25, 4, "< 1 2 3 [4] 5 6 7 >"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(Build(tc.total, tc.page, uint(tc.pageSize)))
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Arrows clamp instead of pointing off the ends.
func TestArrowsClampAtBoundaries(t *testing.T) {
	first := Build(3803, 1, 25)
	if p := first[0]; p.Page != 1 || p.Active {
		t.Fatalf("prev arrow on page 1 = %+v, want page 1 and inactive", p)
	}
	last := Build(3803, 153, 25)
	if n := last[len(last)-1]; n.Page != 153 || n.Active {
		t.Fatalf("next arrow on the last page = %+v, want page 153 and inactive", n)
	}
}

// Out-of-range and degenerate inputs come straight from the query string, so
// they must not panic or produce nonsense.
func TestBuildHandlesHostileInput(t *testing.T) {
	if got := Build(100, 0, 25); render(got) != "< [1] 2 3 4 >" {
		t.Fatalf("page 0 clamped to %q", render(got))
	}
	if got := Build(100, 9999, 25); render(got) != "< 1 2 3 [4] >" {
		t.Fatalf("page past the end clamped to %q", render(got))
	}
	if got := Build(0, 1, 25); render(got) != "< [1] >" {
		t.Fatalf("empty listing rendered %q", render(got))
	}
	if got := Build(-5, 1, 25); render(got) != "< [1] >" {
		t.Fatalf("negative count rendered %q", render(got))
	}
	if got := Build(100, 1, 0); got != nil {
		t.Fatalf("zero page size returned %v, want nil", got)
	}
}

// Exactly one entry is the current page, and it is never a link.
func TestExactlyOneCurrentPage(t *testing.T) {
	for _, page := range []uint{1, 2, 77, 152, 153} {
		var current int
		for _, it := range Build(3803, page, 25) {
			if it.Number && !it.Active {
				current++
				if it.Page != page {
					t.Fatalf("page %d: current marker on page %d", page, it.Page)
				}
			}
		}
		if current != 1 {
			t.Fatalf("page %d: %d current markers, want 1", page, current)
		}
	}
}
