package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/webdav"
)

type ContentDirectory struct {
	BaseDirectory
	*TorrentDirectory
	Library
	pg *cs.PG
}

func (s *ContentDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	lr, err := s.getContentItem(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if lr == nil {
		return nil, nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	return s.TorrentDirectory.Open(ctx, lr.Item.Torrent, lr.NewPath)
}

func (s *ContentDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	lr, err := s.getContentItem(ctx, path)
	if err != nil {
		return nil, err
	}
	if lr == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if isRoot(lr.NewPath) {
		return &webdav.FileInfo{
			Path:    lr.Item.Torrent.Name,
			ModTime: lr.Item.Torrent.CreatedAt,
			IsDir:   true,
		}, nil
	}
	return s.TorrentDirectory.Stat(ctx, lr.Item.Torrent, lr.NewPath)
}

func (s *ContentDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	if isRoot(path) {
		ls, err := s.getContentWithContext(ctx)
		if err != nil {
			return nil, err
		}
		var fis = make([]webdav.FileInfo, len(ls))
		for i, v := range ls {
			fis[i] = s.libraryToFileInfo(v)
		}
		return fis, nil
	}
	lr, err := s.getContentItem(ctx, path)
	if err != nil {
		return nil, err
	}
	if lr == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	return s.TorrentDirectory.ReadDir(ctx, lr.Item.Torrent, lr.NewPath, recursive)
}

func (s *ContentDirectory) getContentWithContext(ctx context.Context) ([]*models.Library, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.GetContent(ctx, db, wcc.User.ID)
}

func (s *ContentDirectory) getContentItem(ctx context.Context, path string) (*ContentItemResponse, error) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	name := parts[0]
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	l, err := models.GetLibraryByTorrentName(ctx, db, wcc.User.ID, name)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	np := strings.TrimPrefix(path, "/"+l.Torrent.Name)
	return &ContentItemResponse{
		Item:    l,
		NewPath: np,
	}, nil
}

type ContentItemResponse struct {
	Item    *models.Library
	NewPath string
}

func (s *ContentDirectory) libraryToFileInfo(l *models.Library) webdav.FileInfo {
	return webdav.FileInfo{
		Path:    l.Torrent.Name,
		ModTime: l.Torrent.CreatedAt,
		IsDir:   true,
	}
}

var _ webdav.FileSystem = (*ContentDirectory)(nil)
