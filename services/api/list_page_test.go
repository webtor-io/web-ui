package api

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

func TestLastListPage(t *testing.T) {
	cases := []struct {
		count  int
		offset uint
		items  int
		want   bool
	}{
		{0, 0, 0, true},
		{250, 0, 100, false},
		{250, 100, 100, false},
		{250, 200, 50, true},
		{150, 100, 50, true},
		// The listing shrank between two pages: the offset is past its end.
		{50, 100, 0, true},
		// An empty page in the middle of the listing.
		{250, 100, 0, true},
		// A page longer than what is left.
		{120, 100, 50, true},
	}
	for _, tc := range cases {
		resp := &ra.ListResponse{Items: make([]ra.ListItem, tc.items), Count: tc.count}
		if got := LastListPage(resp, tc.offset); got != tc.want {
			t.Errorf("count %d, offset %d, %d items: %v, want %v", tc.count, tc.offset, tc.items, got, tc.want)
		}
	}
}
