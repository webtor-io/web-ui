// Package pagination builds the page-link model for listing views.
//
// It lives apart from handlers/resource on purpose: that package pulls in
// services/web and the torrent-store protobufs, which makes its tests
// unrunnable under a plain `go test ./...` (see the proto double-registration
// conflict). Keeping this logic dependency-free means the bound below stays
// covered by the normal suite.
package pagination

// Item is one rendered element of the pager: an arrow, a page number, or an
// elided run.
type Item struct {
	Page   uint
	Active bool
	Prev   bool
	Next   bool
	Number bool
	// Gap marks an elided run of page numbers, rendered as a non-interactive
	// ellipsis.
	Gap bool
}

// Window is how many numbered pages are rendered either side of the current
// one.
//
// Build used to emit a link for EVERY page. Each one becomes a full <a href>
// with an escaped pwd and file query string, so a 3803-file torrent produced
// 153 anchors — tens of KB of markup plus a matching pile of url-escape
// allocations on every listing render. That mattered:
// handlers/resource.(*Handler).get accounts for ~68% of the process's total
// allocation, and in July 2026 a crawler walking exactly such a torrent
// (800-1500 distinct URLs in 10-16 minutes) preceded both web-ui OOM kills.
// Windowing makes the pager's cost constant regardless of torrent size.
const Window = 2

// MaxItems is the upper bound on what Build can return:
// prev + first + gap + (2*Window+1 window entries) + gap + last + next.
const MaxItems = 2*Window + 7

// Build returns the pager for a listing of `total` items shown `pageSize` at a
// time, with `page` current. The result never exceeds MaxItems.
func Build(total int, page uint, pageSize uint) []Item {
	if pageSize == 0 {
		return nil
	}
	if total < 0 {
		total = 0
	}
	// Ceiling division. The previous form (total/pageSize + 1) over-counted by
	// one whenever total was an exact multiple of pageSize, offering a link to
	// a trailing empty page.
	pages := max((uint(total)+pageSize-1)/pageSize, 1)
	// Clamp an out-of-range page for display only; the caller still lists with
	// the page it was given, so a hand-crafted ?page=9999 shows the pager
	// sitting on the last page above an empty listing. Deliberate: clamping the
	// listing query instead would make an out-of-range page do MORE work than
	// an in-range one, which is the wrong incentive for a URL anyone can mint.
	// The previous code marked no page as current at all in this case.
	page = min(max(page, 1), pages)

	prev := max(page-1, 1)
	next := min(page+1, pages)

	number := func(i uint) Item {
		return Item{Page: i, Active: i != page, Number: true}
	}

	res := make([]Item, 0, MaxItems)
	res = append(res, Item{Page: prev, Active: prev != page, Prev: true})

	lo := uint(1)
	if page > Window {
		lo = page - Window
	}
	hi := min(page+Window, pages)

	if lo > 1 {
		res = append(res, number(1))
		if lo > 2 {
			res = append(res, Item{Gap: true})
		}
	}
	for i := lo; i <= hi; i++ {
		res = append(res, number(i))
	}
	if hi < pages {
		if hi < pages-1 {
			res = append(res, Item{Gap: true})
		}
		res = append(res, number(pages))
	}

	res = append(res, Item{Page: next, Active: next != page, Next: true})
	return res
}
