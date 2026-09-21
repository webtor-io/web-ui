package cache_index

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/models"
	vmodels "github.com/webtor-io/web-ui/models/vault"
)

// Availability is what is known to be instantly playable on Webtor among a
// list of torrents: the whole torrent (it is in the Vault), or particular
// files of it (the index saw them complete -- in a seeder or in the Vault --
// within its window).
type Availability struct {
	torrents map[string]bool
	files    map[string]map[int]bool
}

// Cached answers for one release. A release that does not name its file
// (fileIdx < 0) counts as cached when anything of that torrent is: the player
// picks the file later, and "some of it is already here" is the best that can
// be said without it -- the same reading CheckTorrentAvailability gives the
// Stremio list.
func (a *Availability) Cached(hash string, fileIdx int) bool {
	if a == nil {
		return false
	}
	hash = strings.ToLower(hash)
	if a.torrents[hash] {
		return true
	}
	files := a.files[hash]
	if fileIdx < 0 {
		return len(files) > 0
	}
	return files[fileIdx]
}

// Lookup reads the index and the Vault for a list of torrents: two indexed
// queries whatever the length of the list.
func (s *CacheIndex) Lookup(ctx context.Context, hashes []string) (*Availability, error) {
	a := &Availability{torrents: map[string]bool{}, files: map[string]map[int]bool{}}
	ids := make([]string, 0, len(hashes))
	seen := map[string]bool{}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		ids = append(ids, h)
	}
	if len(ids) == 0 {
		return a, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection not available")
	}
	files, err := models.GetCachedFiles(ctx, db, ids, models.StreamingBackendTypeWebtor, s.windows)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cache index")
	}
	for _, f := range files {
		if a.files[f.ResourceID] == nil {
			a.files[f.ResourceID] = map[int]bool{}
		}
		a.files[f.ResourceID][f.FileIdx] = true
	}
	vaulted, err := vmodels.GetVaultedResourceIDs(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range vaulted {
		a.torrents[id] = true
	}
	return a, nil
}

// NewAvailability builds an answer from plain lists -- for the tests of code
// that consumes one.
func NewAvailability(vaulted []string, files map[string][]int) *Availability {
	a := &Availability{torrents: map[string]bool{}, files: map[string]map[int]bool{}}
	for _, h := range vaulted {
		a.torrents[strings.ToLower(h)] = true
	}
	for h, idxs := range files {
		m := map[int]bool{}
		for _, i := range idxs {
			m[i] = true
		}
		a.files[strings.ToLower(h)] = m
	}
	return a
}
