package helpers

import "github.com/webtor-io/web-ui/handlers/library/shared"

type MenuItem struct {
	Title     shared.SectionType
	TargetURL string
	Active    bool
}

type Menu []MenuItem

var baseMenu Menu = Menu{
	{shared.SectionTypeTorrents, "/lib/", false},
	{shared.SectionTypeMovies, "/lib/movies", false},
	{shared.SectionTypeSeries, "/lib/series", false},
}

func (s *VideoContentHelper) MakeMenu(args *shared.IndexArgs) Menu {
	m := Menu{}
	for _, item := range baseMenu {
		nm := item
		if item.Title == args.Section {
			nm.Active = true
		}
		m = append(m, nm)
	}
	return m
}

type MenuHelper struct{}

func NewMenuHelper() *MenuHelper {
	return &MenuHelper{}
}
