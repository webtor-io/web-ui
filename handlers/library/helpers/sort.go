package helpers

import (
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
)

type SortHelper struct{}

func NewSortHelper() *SortHelper {
	return &SortHelper{}
}

type SortOption struct {
	SortType models.SortType
	Title    string
	Value    int
	Selected bool
}

type Sort []SortOption

func NewSort(sortTypes ...models.SortType) Sort {
	var sort Sort
	for _, sortType := range sortTypes {
		sort = append(sort, SortOption{
			SortType: sortType,
			Title:    sortType.String(),
			Value:    int(sortType),
		})
	}
	return sort

}

var videoSort = NewSort(
	models.SortTypeRecentlyAdded, models.SortTypeYear,
	models.SortTypeRating, models.SortTypeName,
)

var sorts = map[shared.SectionType]Sort{
	shared.SectionTypeTorrents: NewSort(models.SortTypeRecentlyAdded, models.SortTypeName),
	shared.SectionTypeMovies:   videoSort,
	shared.SectionTypeSeries:   videoSort,
}

func (s *SortHelper) MakeSort(args *shared.IndexArgs) *Sort {
	sort := sorts[args.Section]
	for k, opt := range sort {
		sort[k].Selected = opt.SortType == args.Sort
	}
	return &sort
}
