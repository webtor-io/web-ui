package backends

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/link_resolver/common"
	rd "github.com/webtor-io/web-ui/services/realdebrid"
)

// resolveLinkResult holds the result of a ResolveLink call
type resolveLinkResult struct {
	url    string
	cached bool
}

// RealDebrid implements Backend interface for RealDebrid
type RealDebrid struct {
	linkCache lazymap.LazyMap[*resolveLinkResult]
	cl        *http.Client
}

// Compile-time check to ensure RealDebrid implements Backend interface
var _ common.Backend = (*RealDebrid)(nil)

// NewRealDebrid creates a new RealDebrid backend
func NewRealDebrid(cl *http.Client) *RealDebrid {
	return &RealDebrid{
		linkCache: lazymap.New[*resolveLinkResult](&lazymap.Config{
			Expire:      15 * time.Minute,
			ErrorExpire: 30 * time.Second,
			Concurrency: 5,
		}),
		cl: cl,
	}
}

// getClient creates a RealDebrid API client with the provided access token
func (s *RealDebrid) getClient(token string) (*rd.Client, error) {
	if token == "" {
		return nil, errors.New("no access token for realdebrid backend")
	}

	// Create RealDebrid client
	return rd.New(s.cl, "https://api.real-debrid.com/rest/1.0", token), nil
}

// getAvailableHost fetches available hosts for torrents and returns the first one
func (s *RealDebrid) getAvailableHost(ctx context.Context, client *rd.Client) (string, error) {
	hosts, err := client.GetTorrentsAvailableHosts(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get available hosts")
	}
	if len(hosts) == 0 {
		return "", nil
	}
	return hosts[0].ID, nil
}

// findMatchingFile searches for a file matching the given path in the torrent files
// and verifies that the file is selected (Selected == 1)
func (s *RealDebrid) findMatchingFile(files []rd.TorrentFile, path string, selected bool) (*rd.TorrentFile, bool) {
	basePath := filepath.Base(path)
	for _, file := range files {
		// Match by exact path or basename, and ensure file is selected
		if (file.Path == path || filepath.Base(file.Path) == basePath || file.Path == basePath) && (file.Selected == 1 || !selected) {
			return &file, true
		}
	}
	return nil, false
}

// ResolveLink generates a direct link using RealDebrid with caching
// Returns the direct download URL and cached status (always true for RealDebrid cached content)
func (s *RealDebrid) ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error) {
	cacheKey := fmt.Sprintf("%s:%s:%s", token, hash, path)

	log.WithFields(log.Fields{
		"hash":      hash,
		"path":      path,
		"cache_key": cacheKey,
	}).Debug("resolving realdebrid link with cache")

	result, err := s.linkCache.Get(cacheKey, func() (*resolveLinkResult, error) {
		log.WithFields(log.Fields{
			"hash": hash,
			"path": path,
		}).Debug("cache miss, performing actual link resolution")

		client, err := s.getClient(token)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create realdebrid client")
		}

		url, cached, err := s.resolveLink(ctx, client, hash, path)
		if err != nil {
			return nil, err
		}

		log.WithFields(log.Fields{
			"hash":   hash,
			"path":   path,
			"url":    url,
			"cached": cached,
		}).Debug("link resolution completed, caching result")

		return &resolveLinkResult{
			url:    url,
			cached: cached,
		}, nil
	})

	if err != nil {
		return "", false, err
	}

	return result.url, result.cached, nil
}

// resolveLink performs the actual link resolution logic
func (s *RealDebrid) resolveLink(ctx context.Context, client *rd.Client, hash, path string) (string, bool, error) {
	upperHash := strings.ToUpper(hash)

	// Step 1: Check if user already has torrent with this infohash
	torrents, err := client.GetAllTorrents(ctx, false)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get user torrents")
	}

	var torrent *rd.TorrentInfo
	var file *rd.TorrentFile
	for i := range torrents {
		if strings.ToUpper(torrents[i].Hash) == upperHash {
			torrent = &torrents[i]
			break
		}
	}

	// If torrent exists in library and is downloaded
	if torrent != nil {
		if torrent.Status == "downloaded" {
			// Find the file matching the path
			var found bool
			file, found = s.findMatchingFile(torrent.Files, path, true)
			if !found {
				return "", false, nil
			}
			log.WithFields(log.Fields{
				"hash":    hash,
				"path":    path,
				"file_id": file.ID,
				"status":  torrent.Status,
			}).Debug("torrent already in library and downloaded")
		} else {
			// Torrent exists but not downloaded
			log.WithFields(log.Fields{
				"hash":   hash,
				"status": torrent.Status,
			}).Debug("torrent in library but not downloaded")
			return "", false, nil
		}
	} else {
		// Step 2: Torrent not in library, add it by magnet
		var host string
		host, err = s.getAvailableHost(ctx, client)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to get available host")
		}

		magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
		var addResp *rd.TorrentAddResponse
		addResp, err = client.AddMagnet(ctx, magnetURL, host)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to add magnet")
		}

		defer func() {
			err = client.DeleteTorrent(ctx, addResp.ID)
			if err != nil {
				log.WithError(err).
					WithField("torrent_id", addResp.ID).
					Warn("failed to delete unavailable torrent")
			}

		}()

		// Get torrent info to find the file to select
		torrent, err = client.GetTorrentInfo(ctx, addResp.ID)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to get torrent info")
		}

		var found bool
		file, found = s.findMatchingFile(torrent.Files, path, false)

		if !found {
			return "", false, nil
		}

		// Select the file
		err = client.SelectTorrentFiles(ctx, torrent.ID, []int{file.ID})
		if err != nil {
			return "", false, errors.Wrap(err, "failed to select file")
		}

		// Get updated torrent info to check status after selection
		torrent, err = client.GetTorrentInfo(ctx, torrent.ID)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to get torrent info after selection")
		}

		// If torrent is not downloaded, it's not available
		if torrent.Status != "downloaded" {
			log.WithFields(log.Fields{
				"hash":   hash,
				"status": torrent.Status,
			}).Debug("torrent not available")
			return "", false, nil
		}

		// Verify the file is selected
		file, found = s.findMatchingFile(torrent.Files, path, true)
		if !found {
			return "", false, nil
		}

		log.WithFields(log.Fields{
			"hash":    hash,
			"path":    path,
			"file_id": file.ID,
			"status":  torrent.Status,
			"cached":  true,
		}).Debug("torrent cached and downloaded")
	}

	// Get torrent info with links
	torrentInfo, err := client.GetTorrentInfo(ctx, torrent.ID)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get torrent info")
	}

	// Find the target link by fileID
	var targetLink string
	for idx, f := range torrentInfo.Files {
		if f.ID == file.ID && file.Selected == 1 && idx < len(torrentInfo.Links) {
			targetLink = torrentInfo.Links[idx]
			break
		}
	}

	if targetLink == "" {
		log.WithField("torrent_id", torrent.ID).Debug("no link available for file")
		return "", false, nil
	}

	// Unrestrict the link
	download, err := client.UnrestrictLink(ctx, targetLink, "", false)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to unrestrict link")
	}

	log.WithFields(log.Fields{
		"hash":   hash,
		"path":   path,
		"url":    download.Download,
		"cached": true,
	}).Info("generated realdebrid link")

	return download.Download, true, nil
}
