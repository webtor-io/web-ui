package webdav

import (
	services "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

func NewFileSystem(pg *services.PG, sapi *api.Api, jobs *job.Handler, sep string) webdav.FileSystem {
	td := &TorrentDirectory{
		api: sapi,
	}
	return &DebugDirectory{
		Inner: &PrefixDirectory{
			Separator: sep,
			Inner: &RootDirectory{
				Children: map[string]webdav.FileSystem{
					"torrents": &TorrentLibraryDirectory{
						pg:   pg,
						api:  sapi,
						jobs: jobs,
					},
					"all": &ContentDirectory{
						Library:          &AllLibrary{},
						TorrentDirectory: td,
						pg:               pg,
					},
					"movies": &ContentDirectory{
						Library:          &MovieLibrary{},
						TorrentDirectory: td,
						pg:               pg,
					},
					"series": &ContentDirectory{
						Library:          &SeriesLibrary{},
						TorrentDirectory: td,
						pg:               pg,
					},
				},
			},
		},
	}
}
