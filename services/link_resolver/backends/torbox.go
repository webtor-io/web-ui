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
	tb "github.com/webtor-io/web-ui/services/torbox"
)

// resolveLinkResultTorbox holds the result of a ResolveLink call
type resolveLinkResultTorbox struct {
	url    string
	cached bool
}

// Torbox implements Backend interface for Torbox
type Torbox struct {
	linkCache lazymap.LazyMap[*resolveLinkResultTorbox]
	cl        *http.Client
}

func (s *Torbox) Validate(ctx context.Context, token string) error {
	cl, err := s.getClient(token)
	if err != nil {
		return err
	}
	_, err = cl.GetUser(ctx)
	if err != nil {
		return err
	}
	return nil
}

// Compile-time check to ensure Torbox implements Backend interface
var _ common.Backend = (*Torbox)(nil)

// NewTorbox creates a new Torbox backend
func NewTorbox(cl *http.Client) *Torbox {
	return &Torbox{
		linkCache: lazymap.New[*resolveLinkResultTorbox](&lazymap.Config{
			Expire:      15 * time.Minute,
			ErrorExpire: 30 * time.Second,
			Concurrency: 5,
		}),
		cl: cl,
	}
}

// getClient creates a Torbox API client with the provided access token
func (s *Torbox) getClient(token string) (*tb.Client, error) {
	if token == "" {
		return nil, errors.New("no access token for torbox backend")
	}

	// Create Torbox client
	return tb.NewClient(s.cl, "https://api.torbox.app", token), nil
}

// findMatchingFile searches for a file matching the given path in the torrent files
func (s *Torbox) findMatchingFile(files []tb.File, path string) (*tb.File, bool) {
	basePath := filepath.Base(path)
	for _, file := range files {
		// Match by exact name, basename, or short name
		if file.Name == path || filepath.Base(file.Name) == basePath ||
			file.ShortName == path || file.ShortName == basePath {
			return &file, true
		}
	}
	return nil, false
}

// ResolveLink generates a direct link using Torbox with caching
// Returns the direct download URL and cached status
func (s *Torbox) ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error) {
	cacheKey := fmt.Sprintf("%s:%s:%s", token, hash, path)

	log.WithFields(log.Fields{
		"hash":      hash,
		"path":      path,
		"cache_key": cacheKey,
	}).Debug("resolving torbox link with cache")

	result, err := s.linkCache.Get(cacheKey, func() (*resolveLinkResultTorbox, error) {
		log.WithFields(log.Fields{
			"hash": hash,
			"path": path,
		}).Debug("cache miss, performing actual link resolution")

		client, err := s.getClient(token)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create torbox client")
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

		return &resolveLinkResultTorbox{
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
func (s *Torbox) resolveLink(ctx context.Context, client *tb.Client, hash, path string) (string, bool, error) {
	cached, err := client.CheckCached(ctx, []string{hash}, "", true)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to check cached status")
	}
	// If not cached, content is not available
	if len(cached) == 0 || cached[0].Hash == "" {
		log.WithField("hash", hash).Debug("torrent not cached")
		return "", false, nil
	}

	var found bool
	_, found = s.findMatchingFile(cached[0].Files, path)
	if !found {
		return "", false, nil
	}

	upperHash := strings.ToUpper(hash)

	// Check if user already has torrent with this infohash
	torrents, err := client.ListTorrents(ctx, 0)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get user torrents")
	}

	var torrent *tb.Torrent
	var file *tb.File
	for i := range torrents {
		if strings.ToUpper(torrents[i].Hash) == upperHash {
			torrent = &torrents[i]
			break
		}
	}

	// If torrent exists in library and is downloaded
	if torrent != nil {
		if torrent.DownloadFinished {
			// Find the file matching the path
			var found bool
			file, found = s.findMatchingFile(torrent.Files, path)
			if !found {
				return "", false, nil
			}
			log.WithFields(log.Fields{
				"hash":              hash,
				"path":              path,
				"file_id":           file.ID,
				"download_finished": torrent.DownloadFinished,
			}).Debug("torrent already in library and downloaded")
		} else {
			// Torrent exists but not downloaded
			log.WithFields(log.Fields{
				"hash":              hash,
				"download_finished": torrent.DownloadFinished,
			}).Debug("torrent in library but not downloaded")
			return "", false, nil
		}
	} else {

		// Create torrent from magnet
		magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
		createResp, err := client.CreateTorrent(ctx, magnetURL)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to create torrent")
		}

		defer func(tid int) {
			err = client.ControlTorrent(ctx, tid, "delete")
			if err != nil {
				log.WithError(err).
					WithField("torrent_id", tid).
					Warn("failed to delete temporary torrent")
			}
		}(createResp.TorrentID)

		// fetch the created torrent from user list to obtain files and status
		torrents, err := client.ListTorrents(ctx, createResp.TorrentID)
		if err != nil {
			return "", false, errors.Wrap(err, "failed to get user torrents after creation")
		}
		for i := range torrents {
			if torrents[i].ID == createResp.TorrentID || strings.ToUpper(torrents[i].Hash) == strings.ToUpper(createResp.Hash) {
				torrent = &torrents[i]
				break
			}
		}
		if torrent == nil {
			return "", false, errors.New("created torrent not found in user list")
		}

		// Wait for torrent to be downloaded (cached torrents should download instantly)
		// Since it's cached, we'll check if it's already finished
		if !torrent.DownloadFinished {
			log.WithFields(log.Fields{
				"hash":              hash,
				"download_finished": torrent.DownloadFinished,
			}).Debug("torrent not available yet")
			return "", false, nil
		}

		// Find the file in the created torrent
		file, found = s.findMatchingFile(torrent.Files, path)
		if !found {
			return "", false, nil
		}

		log.WithFields(log.Fields{
			"hash":    hash,
			"path":    path,
			"file_id": file.ID,
			"cached":  true,
		}).Debug("torrent cached and downloaded")
	}

	// Request download link
	downloadURL, err := client.RequestDownloadLink(ctx, torrent.ID, file.ID)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to request download link")
	}

	if downloadURL == "" {
		log.WithField("torrent_id", torrent.ID).Debug("no download link available for file")
		return "", false, nil
	}

	log.WithFields(log.Fields{
		"hash":   hash,
		"path":   path,
		"url":    downloadURL,
		"cached": true,
	}).Info("generated torbox link")

	return downloadURL, true, nil
}
