package shared

import "github.com/webtor-io/web-ui/models"

// WatchedFilter is a tri-state filter for movie/series listings in the library:
// "" (all), "unwatched" (exclude watched), "watched" (only watched).
type WatchedFilter string

const (
	WatchedFilterAll       WatchedFilter = ""
	WatchedFilterUnwatched WatchedFilter = "unwatched"
	WatchedFilterWatched   WatchedFilter = "watched"
)

type IndexArgs struct {
	Sort    models.SortType
	Section SectionType
	Watched WatchedFilter
}
