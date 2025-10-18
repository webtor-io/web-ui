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

// availabilityResult holds the result of an availability check
type availabilityResult struct {
	fileID    int
	available bool
}

// RealDebrid implements Backend interface for RealDebrid
type RealDebrid struct {
	availabilityCache lazymap.LazyMap[*availabilityResult]
	cl                *http.Client
}

// Compile-time check to ensure RealDebrid implements Backend interface
var _ common.Backend = (*RealDebrid)(nil)

// NewRealDebrid creates a new RealDebrid backend
func NewRealDebrid(cl *http.Client) *RealDebrid {
	return &RealDebrid{
		availabilityCache: lazymap.New[*availabilityResult](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 30 * time.Second,
			Concurrency: 1,
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

// checkAvailabilityCached is a cached variant that uses LazyMap to cache availability check results
func (s *RealDebrid) checkAvailabilityCached(ctx context.Context, client *rd.Client, hash, path string) (int, bool, error) {
	cacheKey := fmt.Sprintf("%s:%s", hash, path)

	log.WithFields(log.Fields{
		"hash":      hash,
		"path":      path,
		"cache_key": cacheKey,
	}).Debug("checking realdebrid availability with cache")

	result, err := s.availabilityCache.Get(cacheKey, func() (*availabilityResult, error) {
		log.WithFields(log.Fields{
			"hash": hash,
			"path": path,
		}).Debug("cache miss, performing actual availability check")

		fileID, available, err := s.checkAvailability(ctx, client, hash, path)
		if err != nil {
			return nil, err
		}

		log.WithFields(log.Fields{
			"hash":      hash,
			"path":      path,
			"file_id":   fileID,
			"available": available,
		}).Debug("availability check completed, caching result")

		return &availabilityResult{
			fileID:    fileID,
			available: available,
		}, nil
	})

	if err != nil {
		return 0, false, err
	}

	return result.fileID, result.available, nil
}

// CheckAvailability checks if content is available in RealDebrid
// It verifies instant availability of the torrent and matches the requested file path
func (s *RealDebrid) CheckAvailability(ctx context.Context, token, hash, path string) (bool, error) {
	client, err := s.getClient(token)
	if err != nil {
		return false, errors.Wrap(err, "failed to create realdebrid client")
	}
	_, available, err := s.checkAvailabilityCached(ctx, client, hash, path)
	return available, err
}

func (s *RealDebrid) checkAvailability(ctx context.Context, client *rd.Client, hash, path string) (int, bool, error) {
	upperHash := strings.ToUpper(hash)

	// Step 1: Check if user already has torrent with this infohash
	torrents, err := client.GetAllTorrents(ctx, false)
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to get user torrents")
	}

	var existingTorrent *rd.TorrentInfo
	for i := range torrents {
		if strings.ToUpper(torrents[i].Hash) == upperHash {
			existingTorrent = &torrents[i]
			break
		}
	}

	// If torrent exists in library and is downloaded, it's available
	if existingTorrent != nil {
		if existingTorrent.Status == "downloaded" {
			// Find the file matching the path
			fileID, found := s.findMatchingFile(existingTorrent.Files, path)
			if found {
				log.WithFields(log.Fields{
					"hash":    hash,
					"path":    path,
					"file_id": fileID,
					"status":  existingTorrent.Status,
				}).Debug("torrent already in library and downloaded")
				return fileID, true, nil
			}
		}
		// Torrent exists but not downloaded or file not found
		log.WithFields(log.Fields{
			"hash":   hash,
			"status": existingTorrent.Status,
		}).Debug("torrent in library but not downloaded")
		return 0, false, nil
	}

	// Step 2: Torrent not in library, add it by magnet to test availability
	// Get available host for the torrent
	host, err := s.getAvailableHost(ctx, client)
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to get available host")
	}

	magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
	addResp, err := client.AddMagnet(ctx, magnetURL, host)
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to add magnet")
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
	torrentInfo, err := client.GetTorrentInfo(ctx, addResp.ID)
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to get torrent info")
	}

	// Find the matching file (without checking if it's selected yet)
	basePath := filepath.Base(path)
	var fileID int
	for _, file := range torrentInfo.Files {
		if file.Path == path || filepath.Base(file.Path) == basePath || file.Path == basePath {
			fileID = file.ID
			break
		}
	}

	// Select the file
	err = client.SelectTorrentFiles(ctx, addResp.ID, []int{fileID})
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to select file")
	}

	// Get updated torrent info to check status after selection
	torrentInfo, err = client.GetTorrentInfo(ctx, addResp.ID)
	if err != nil {
		return 0, false, errors.Wrap(err, "failed to get torrent info after selection")
	}

	// If torrent is already downloaded (instant availability), it's available
	if torrentInfo.Status == "downloaded" {
		// Verify the file is now selected
		fileID, found := s.findMatchingFile(torrentInfo.Files, path)
		if found {
			log.WithFields(log.Fields{
				"hash":    hash,
				"path":    path,
				"file_id": fileID,
				"status":  torrentInfo.Status,
				"cached":  true,
			}).Debug("torrent cached and downloaded")
			// Delete since it wasn't in library originally
			_ = client.DeleteTorrent(ctx, addResp.ID)
			return fileID, true, nil
		}
	}

	// Step 3: Not downloaded
	log.WithFields(log.Fields{
		"hash":   hash,
		"status": torrentInfo.Status,
	}).Debug("torrent not available")

	if err != nil {
		log.WithError(err).
			WithField("torrent_id", addResp.ID).
			Warn("failed to delete unavailable torrent")
	}

	return 0, false, nil
}

// findMatchingFile searches for a file matching the given path in the torrent files
// and verifies that the file is selected (Selected == 1)
func (s *RealDebrid) findMatchingFile(files []rd.TorrentFile, path string) (int, bool) {
	basePath := filepath.Base(path)
	for _, file := range files {
		// Match by exact path or basename, and ensure file is selected
		if (file.Path == path || filepath.Base(file.Path) == basePath || file.Path == basePath) && file.Selected == 1 {
			return file.ID, true
		}
	}
	return 0, false
}

// ResolveLink generates a direct link using RealDebrid
// It follows the same logic as checkAvailability to ensure consistency
// Returns the direct download URL and cached status (always true for RealDebrid cached content)
func (s *RealDebrid) ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error) {
	log.WithFields(log.Fields{
		"hash": hash,
		"path": path,
	}).Debug("resolving realdebrid link")

	client, err := s.getClient(token)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to create realdebrid client")
	}

	upperHash := strings.ToUpper(hash)

	// Step 1: Check if user already has torrent with this infohash
	torrents, err := client.GetAllTorrents(ctx, false)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get user torrents")
	}

	var existingTorrent *rd.TorrentInfo
	for i := range torrents {
		if strings.ToUpper(torrents[i].Hash) == upperHash {
			existingTorrent = &torrents[i]
			break
		}
	}

	var shouldDelete bool
	var torrentID string
	var fileID int

	// If torrent exists in library and is downloaded
	if existingTorrent != nil {
		if existingTorrent.Status == "downloaded" {
			// Find the file matching the path
			fID, found := s.findMatchingFile(existingTorrent.Files, path)
			if !found {
				return "", false, nil
			}
			fileID = fID
			torrentID = existingTorrent.ID
			shouldDelete = false

			log.WithFields(log.Fields{
				"hash":    hash,
				"path":    path,
				"file_id": fileID,
				"status":  existingTorrent.Status,
			}).Debug("torrent already in library and downloaded")
		} else {
			// Torrent exists but not downloaded
			log.WithFields(log.Fields{
				"hash":   hash,
				"status": existingTorrent.Status,
			}).Debug("torrent in library but not downloaded")
			return "", false, nil
		}
	} else {
		// Step 2: Torrent not in library, add it by magnet
		host, err := s.getAvailableHost(ctx, client)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to get available host")
		}

		magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
		addResp, err := client.AddMagnet(ctx, magnetURL, host)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to add magnet")
		}

		torrentID = addResp.ID
		shouldDelete = true

		// Get torrent info to find the file to select
		torrentInfo, err := client.GetTorrentInfo(ctx, torrentID)
		if err != nil {
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, errors.Wrap(err, "failed to get torrent info")
		}

		// Find the matching file
		basePath := filepath.Base(path)
		var found bool
		for _, file := range torrentInfo.Files {
			if file.Path == path || filepath.Base(file.Path) == basePath || file.Path == basePath {
				fileID = file.ID
				found = true
				break
			}
		}

		if !found {
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, nil
		}

		// Select the file
		err = client.SelectTorrentFiles(ctx, torrentID, []int{fileID})
		if err != nil {
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, errors.Wrap(err, "failed to select file")
		}

		// Get updated torrent info to check status after selection
		torrentInfo, err = client.GetTorrentInfo(ctx, torrentID)
		if err != nil {
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, errors.Wrap(err, "failed to get torrent info after selection")
		}

		// If torrent is not downloaded, it's not available
		if torrentInfo.Status != "downloaded" {
			log.WithFields(log.Fields{
				"hash":   hash,
				"status": torrentInfo.Status,
			}).Debug("torrent not available")
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, nil
		}

		// Verify the file is selected
		fID, found := s.findMatchingFile(torrentInfo.Files, path)
		if !found {
			_ = client.DeleteTorrent(ctx, torrentID)
			return "", false, nil
		}
		fileID = fID

		log.WithFields(log.Fields{
			"hash":    hash,
			"path":    path,
			"file_id": fileID,
			"status":  torrentInfo.Status,
			"cached":  true,
		}).Debug("torrent cached and downloaded")
	}

	// Get torrent info with links
	torrentInfo, err := client.GetTorrentInfo(ctx, torrentID)
	if err != nil {
		if shouldDelete {
			_ = client.DeleteTorrent(ctx, torrentID)
		}
		return "", false, errors.Wrap(err, "failed to get torrent info")
	}

	// Find the target link by fileID
	var targetLink string
	for idx, file := range torrentInfo.Files {
		if file.ID == fileID && file.Selected == 1 && idx < len(torrentInfo.Links) {
			targetLink = torrentInfo.Links[idx]
			break
		}
	}

	if targetLink == "" {
		log.WithField("torrent_id", torrentID).Debug("no link available for file")
		if shouldDelete {
			_ = client.DeleteTorrent(ctx, torrentID)
		}
		return "", false, nil
	}

	// Unrestrict the link
	download, err := client.UnrestrictLink(ctx, targetLink, "", false)
	if err != nil {
		if shouldDelete {
			_ = client.DeleteTorrent(ctx, torrentID)
		}
		return "", false, errors.Wrap(err, "failed to unrestrict link")
	}

	// Delete if it wasn't in library originally
	if shouldDelete {
		err = client.DeleteTorrent(ctx, torrentID)
		if err != nil {
			log.WithError(err).
				WithField("torrent_id", torrentID).
				Warn("failed to delete temporary torrent")
		}
	}

	log.WithFields(log.Fields{
		"hash":   hash,
		"path":   path,
		"url":    download.Download,
		"cached": true,
	}).Info("generated realdebrid link")

	return download.Download, true, nil
}
