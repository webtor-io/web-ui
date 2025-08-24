package webdav

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	services "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

type TorrentLibraryDirectory struct {
	*BaseDirectory
	pg   *services.PG
	api  *api.Api
	jobs *job.Handler
}

func (s *TorrentLibraryDirectory) Open(ctx context.Context, name string) (io.ReadCloser, *url.URL, error) {
	l, err := s.getLibraryByName(ctx, torrentToName(name))
	if err != nil {
		return nil, nil, err
	}
	if l == nil {
		return nil, nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	return s.readTorrent(ctx, l)

}

func (s *TorrentLibraryDirectory) Stat(ctx context.Context, name string) (*webdav.FileInfo, error) {
	l, err := s.getLibraryByName(ctx, torrentToName(name))
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	fi := s.libraryToFileInfo(l)
	return &fi, nil
}

func (s *TorrentLibraryDirectory) ReadDir(ctx context.Context, name string, recursive bool) ([]webdav.FileInfo, error) {
	ls, err := s.getLibraryList(ctx)
	if err != nil {
		return nil, err
	}
	fi := make([]webdav.FileInfo, len(ls))
	for i, v := range ls {
		fi[i] = s.libraryToFileInfo(v)
	}
	return fi, nil
}

func (s *TorrentLibraryDirectory) Create(ctx context.Context, name string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	if !strings.HasSuffix(name, ".torrent") {
		return nil, false, webdav.NewHTTPError(400, errors.New("bad request"))
	}

	t, err := io.ReadAll(body)
	if err != nil {
		return nil, false, err
	}
	resp, info, err := s.storeTorrentToAPI(ctx, t)
	if err != nil {
		return nil, false, err
	}
	size := int64(len(t))
	l, err := s.storeToLibrary(ctx, resp.ID, info, torrentToName(name), size)
	if err != nil {
		return nil, false, err
	}

	fi := s.libraryToFileInfoNew(l, size)
	return &fi, true, nil
}

func (s *TorrentLibraryDirectory) RemoveAll(ctx context.Context, name string, opts *webdav.RemoveAllOptions) error {
	l, err := s.getLibraryByName(ctx, torrentToName(name))
	if err != nil {
		return err
	}
	if l == nil {
		return webdav.NewHTTPError(404, errors.New("file not found"))
	}
	return s.removeFromLibrary(ctx, l)
}

func (s *TorrentLibraryDirectory) Move(ctx context.Context, name, dest string, options *webdav.MoveOptions) (bool, error) {
	ls, err := s.getLibraryByName(ctx, torrentToName(name))
	if err != nil {
		return false, err
	}
	if ls == nil {
		return false, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	ls.Name = torrentToName(filepath.Base(dest))
	err = s.updateLibraryName(ctx, ls)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *TorrentLibraryDirectory) storeToLibrary(ctx context.Context, resourceID string, info *metainfo.Info, name string, torrentSize int64) (*models.Library, error) {
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	l, err := models.AddTorrentToLibrary(ctx, db, wcc.User.ID, resourceID, info, name, torrentSize)
	if err != nil {
		return nil, err
	}
	_, _ = s.jobs.Enrich(wcc, resourceID)
	return l, nil
}

func (s *TorrentLibraryDirectory) storeTorrentToAPI(ctx context.Context, t []byte) (*ra.ResourceResponse, *metainfo.Info, error) {
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	b := io.NopCloser(bytes.NewReader(t))
	mi, err := metainfo.Load(b)
	if err != nil {
		return nil, nil, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.api.StoreResource(ctx, wcc.ApiClaims, t)
	if err != nil {
		return nil, nil, err
	}
	return resp, &info, nil
}

func (s *TorrentLibraryDirectory) readTorrent(ctx context.Context, l *models.Library) (io.ReadCloser, *url.URL, error) {
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.api.GetTorrentCached(ctx, wcc.ApiClaims, l.Torrent.ResourceID)
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(resp)), nil, nil
}

func (s *TorrentLibraryDirectory) removeFromLibrary(ctx context.Context, l *models.Library) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return err
	}
	return models.RemoveFromLibrary(ctx, db, wcc.User.ID, l.Torrent.ResourceID)
}

func (s *TorrentLibraryDirectory) updateLibraryName(ctx context.Context, l *models.Library) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	return models.UpdateLibraryName(ctx, db, l)
}

func (s *TorrentLibraryDirectory) getLibraryList(ctx context.Context) ([]*models.Library, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	return models.GetLibraryTorrentsList(ctx, db, wcc.User.ID, models.SortTypeName)
}

func (s *TorrentLibraryDirectory) getLibraryByName(ctx context.Context, name string) (*models.Library, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	return models.GetLibraryByName(ctx, db, wcc.User.ID, name)
}

var _ webdav.FileSystem = (*TorrentLibraryDirectory)(nil)

func torrentToName(path string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".torrent")
}

func (s *TorrentLibraryDirectory) libraryToFileInfo(l *models.Library) webdav.FileInfo {
	return webdav.FileInfo{
		Path:     l.Name + ".torrent",
		ModTime:  l.CreatedAt,
		MIMEType: "application/x-bittorrent",
		Size:     l.Torrent.TorrentSizeBytes,
	}
}

func (s *TorrentLibraryDirectory) libraryToFileInfoNew(l *models.Library, size int64) webdav.FileInfo {
	return webdav.FileInfo{
		Path:     l.Name + ".torrent",
		ModTime:  l.CreatedAt,
		MIMEType: "application/x-bittorrent",
		Size:     size,
	}
}
