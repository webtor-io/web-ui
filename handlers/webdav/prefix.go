package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/webtor-io/web-ui/services/webdav"
)

type PrefixDirectory struct {
	BaseDirectory
	Separator string
	Inner     webdav.FileSystem
}

func (s *PrefixDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	_, newPath := s.splitPath(path)
	return s.Inner.Open(ctx, newPath)
}

func (s *PrefixDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	prefix, newPath := s.splitPath(path)
	fis, err := s.Inner.ReadDir(ctx, newPath, recursive)
	if err != nil {
		return nil, err
	}
	return addPrefixes(fis, prefix), nil
}

func (s *PrefixDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	prefix, newPath := s.splitPath(path)
	fi, err := s.Inner.Stat(ctx, newPath)
	if err != nil {
		return nil, err
	}
	return addPrefix(fi, prefix), nil
}

func (s *PrefixDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	_, newPath := s.splitPath(path)
	return s.Inner.RemoveAll(ctx, newPath, opts)
}

func (s *PrefixDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	prefix, newPath := s.splitPath(path)
	fi, ok, err := s.Inner.Create(ctx, newPath, body, opts)
	if err != nil {
		return nil, false, err
	}
	return addPrefix(fi, prefix), ok, nil
}

func (s *PrefixDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	_, newPath := s.splitPath(path)
	return s.Inner.Move(ctx, newPath, dest, options)
}

func (s *PrefixDirectory) splitPath(path string) (string, string) {
	parts := strings.SplitN(path, s.Separator, 2)
	prefix := parts[0] + s.Separator
	newPath := parts[1]
	return prefix, newPath
}

var _ webdav.FileSystem = (*PrefixDirectory)(nil)
