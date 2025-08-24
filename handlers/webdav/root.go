package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/webdav"
)

type RootDirectory struct {
	BaseDirectory
	Children map[string]webdav.FileSystem
}

func (s *RootDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	c := s.getChild(path)
	if c == nil {
		return nil, nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return nil, nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Open(ctx, c.NewPath)
}

func (s *RootDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	if isRoot(path) {
		var dirs []webdav.FileInfo
		for k, _ := range s.Children {
			dirs = append(dirs, newDirectoryFileInfo(k))
		}
		return dirs, nil
	}
	c := s.getChild(path)
	if c == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	return c.Child.ReadDir(ctx, c.NewPath, recursive)
}

func (s *RootDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	if isRoot(path) {
		fi := newDirectoryFileInfo("/")
		return &fi, nil
	}
	c := s.getChild(path)
	if c == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		fi := newDirectoryFileInfo(c.Name)
		return &fi, nil
	}
	return c.Child.Stat(ctx, c.NewPath)
}

func (s *RootDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	if isRoot(path) {
		return webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	c := s.getChild(path)
	if c == nil {
		return webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.RemoveAll(ctx, c.NewPath, opts)
}
func (s *RootDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	c := s.getChild(path)
	if c == nil {
		return nil, false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	if c.Root == path {
		return nil, false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Create(ctx, c.NewPath, body, opts)
}
func (s *RootDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	c := s.getChild(path)
	if c == nil {
		return false, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Move(ctx, c.NewPath, s.removePrefix(dest, c.Name), options)
}

type ChildResponse struct {
	Child   webdav.FileSystem
	Name    string
	NewPath string
	Root    string
}

func (s *RootDirectory) getChild(path string) *ChildResponse {
	for name, d := range s.Children {
		cr := "/" + name + "/"
		if strings.HasPrefix(path, cr) {
			return &ChildResponse{
				Child:   d,
				Name:    path,
				NewPath: s.removePrefix(path, name),
				Root:    cr,
			}
		}
	}
	return nil
}

func (s *RootDirectory) removePrefix(path string, name string) string {
	return strings.TrimPrefix(path, "/"+name)
}
