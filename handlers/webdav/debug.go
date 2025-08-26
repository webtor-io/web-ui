package webdav

import (
	"context"
	"io"
	"net/url"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/webdav"
)

type DebugDirectory struct {
	BaseDirectory
	Debug bool
	Inner webdav.FileSystem
}

func (s *DebugDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	ic, u, err := s.Inner.Open(ctx, path)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("open")
		return nil, nil, err
	}
	log.WithField("path", path).Info("open")
	return ic, u, nil
}

func (s *DebugDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	fis, err := s.Inner.ReadDir(ctx, path, recursive)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("read dir")
		return nil, err
	}
	log.WithField("path", path).WithField("files", fis).Info("read dir")
	return fis, nil
}

func (s *DebugDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	fi, err := s.Inner.Stat(ctx, path)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("stat")
		return nil, err
	}
	log.WithField("path", path).WithField("file", fi).Info("stat")
	return fi, nil
}

func (s *DebugDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	err := s.Inner.RemoveAll(ctx, path, opts)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("remove all")
		return err
	}
	log.WithField("path", path).Info("remove all")
	return nil
}

func (s *DebugDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	fi, ok, err := s.Inner.Create(ctx, path, body, opts)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("create")
		return nil, false, err
	}
	log.WithField("path", path).WithField("file", fi).Info("create")
	return fi, ok, nil
}

func (s *DebugDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	ok, err := s.Inner.Move(ctx, path, dest, options)
	if err != nil {
		log.WithError(err).WithField("path", path).Error("move")
		return false, err
	}
	log.WithField("path", path).WithField("dest", dest).Info("move")
	return ok, nil
}

var _ webdav.FileSystem = (*DebugDirectory)(nil)
