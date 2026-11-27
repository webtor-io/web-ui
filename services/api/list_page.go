package api

import ra "github.com/webtor-io/rest-api/services"

// LastListPage reports whether a /list page ends the walk: the page is empty or reaches the count.
// rest-api answers an offset past the end with 200, no items and the current count, so a listing that
// shrank between two pages never meets count-offset == len(items).
func LastListPage(resp *ra.ListResponse, offset uint) bool {
	return len(resp.Items) == 0 || resp.Count-int(offset) <= len(resp.Items)
}
