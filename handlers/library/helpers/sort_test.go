package helpers

import (
	"testing"

	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
)

// The options are shared by every request; marking the selected one must
// not show through in another request's options.
func TestMakeSortDoesNotShareSelection(t *testing.T) {
	h := NewSortHelper()
	year := h.MakeSort(&shared.IndexArgs{Section: shared.SectionTypeMovies, Sort: models.SortTypeYear})
	h.MakeSort(&shared.IndexArgs{Section: shared.SectionTypeSeries, Sort: models.SortTypeName})
	for _, o := range *year {
		if o.Selected != (o.SortType == models.SortTypeYear) {
			t.Errorf("%s: selected=%v after another request's MakeSort", o.Title, o.Selected)
		}
	}
}
