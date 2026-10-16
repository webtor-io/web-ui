package web

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// MaxBulkIDs bounds one "remove selected" request (the library and Vault
// pages, partials/bulk_select.html). Over it the request is refused rather
// than cut short: removing part of a selection without saying so is worse.
// app/bulk_select.js mirrors it, so "Select all" stops here and a library
// larger than this goes in rounds.
const MaxBulkIDs = 1000

var ErrTooManyIDs = errors.New("too many items selected")

// FormBulkIDs reads the ids a remove form sends: repeated resource_id (the
// resource page's single remove) and the comma-joined resource_ids
// (app/bulk_select.js). The selection rides in ONE field because async.js
// posts multipart, and Go's multipart reader stops at 1000 parts -- gin
// swallows that error, so a box per part would arrive silently cut to
// nothing or to a tail, past the reach of the bound above.
func FormBulkIDs(c *gin.Context) ([]string, error) {
	return BulkIDs(append(c.PostFormArray("resource_id"), strings.Split(c.PostForm("resource_ids"), ",")...))
}

// BulkIDs returns the distinct non-empty values, in the order sent. Distinct,
// because Vault removes pledge by pledge and the second removal of the same
// one would fail as "pledge not found".
func BulkIDs(vals []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		if len(out) == MaxBulkIDs {
			return nil, ErrTooManyIDs
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}
