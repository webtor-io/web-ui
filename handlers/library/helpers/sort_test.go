package helpers

import (
	"reflect"
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

// My files also sorts by the last change; Torrents keeps its two sorts.
func TestMakeSortUploads(t *testing.T) {
	h := NewSortHelper()
	for section, want := range map[shared.SectionType][]models.SortType{
		shared.SectionTypeUploads:  {models.SortTypeRecentlyAdded, models.SortTypeRecentlyUpdated, models.SortTypeName},
		shared.SectionTypeTorrents: {models.SortTypeRecentlyAdded, models.SortTypeName},
	} {
		var got []models.SortType
		for _, o := range *h.MakeSort(&shared.IndexArgs{Section: section}) {
			got = append(got, o.SortType)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", section, got, want)
		}
	}
}
