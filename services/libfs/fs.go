// Package libfs implements the user's library as a virtual filesystem
// (services/vfs.FileSystem).
//
// It is protocol-neutral on purpose: WebDAV (services/webdav) and S3
// (services/s3) both mount this exact tree, so the folder layout a user sees is
// identical whichever one they connect with. Nothing here may import a protocol
// package.
package libfs

import (
	services "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/vfs"
)

// Root directory names. They are the contract every protocol exposes verbatim —
// WebDAV shows them as the top-level folders, S3 as the bucket list.
const (
	RootTorrents = "torrents"
	RootAll      = "all"
	RootMovies   = "movies"
	RootSeries   = "series"
)

// New builds the library tree. The caller owns any protocol-specific wrapping —
// handlers/webdav puts a PrefixDirectory on top because its URLs carry an alias
// prefix, while S3 addresses the tree directly as bucket + key.
func New(pg *services.PG, sapi *api.Api, jobs *j.Jobs) vfs.FileSystem {
	td := &TorrentDirectory{
		api: sapi,
	}
	return &DebugDirectory{
		Inner: &RootDirectory{
			Children: map[string]vfs.FileSystem{
				RootTorrents: &TorrentLibraryDirectory{
					pg:   pg,
					api:  sapi,
					jobs: jobs,
				},
				RootAll: &ContentDirectory{
					Library:          &AllLibrary{},
					TorrentDirectory: td,
					pg:               pg,
				},
				RootMovies: &ContentDirectory{
					Library:          &MovieLibrary{},
					TorrentDirectory: td,
					pg:               pg,
				},
				RootSeries: &ContentDirectory{
					Library:          &SeriesLibrary{},
					TorrentDirectory: td,
					pg:               pg,
				},
			},
		},
	}
}
