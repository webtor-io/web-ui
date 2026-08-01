package libfs

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/vfs"
)

type RootDirectory struct {
	BaseDirectory
	Children map[string]vfs.FileSystem
}

func (s *RootDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	c := s.getChild(path)
	if c == nil {
		return nil, nil, vfs.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return nil, nil, vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Open(ctx, c.NewPath)
}

func (s *RootDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]vfs.FileInfo, error) {
	if isRoot(path) {
		var dirs []vfs.FileInfo
		for k, _ := range s.Children {
			dirs = append(dirs, newDirectoryFileInfo(k))
		}
		return dirs, nil
	}
	c := s.getChild(path)
	if c == nil {
		return nil, vfs.NewHTTPError(404, errors.New("file not found"))
	}
	fis, err := c.Child.ReadDir(ctx, c.NewPath, recursive)
	if err != nil {
		return nil, err
	}
	return AddPrefixes(fis, c.Root), nil
}

func (s *RootDirectory) Stat(ctx context.Context, path string) (*vfs.FileInfo, error) {
	if isRoot(path) {
		fi := newDirectoryFileInfo("/")
		return &fi, nil
	}
	c := s.getChild(path)
	if c == nil {
		return nil, vfs.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		fi := newDirectoryFileInfo(c.Name)
		return &fi, nil
	}
	fi, err := c.Child.Stat(ctx, c.NewPath)
	if err != nil {
		return nil, err
	}
	return AddPrefix(fi, c.Root), nil
}

func (s *RootDirectory) RemoveAll(ctx context.Context, path string, opts *vfs.RemoveAllOptions) error {
	if isRoot(path) {
		return vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	c := s.getChild(path)
	if c == nil {
		return vfs.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.RemoveAll(ctx, c.NewPath, opts)
}
func (s *RootDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *vfs.CreateOptions) (*vfs.FileInfo, bool, error) {
	c := s.getChild(path)
	if c == nil {
		return nil, false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	if c.Root == path {
		return nil, false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	fi, ok, err := c.Child.Create(ctx, c.NewPath, body, opts)
	if err != nil {
		return nil, false, err
	}
	return AddPrefix(fi, c.Root), ok, nil
}
func (s *RootDirectory) Move(ctx context.Context, path, dest string, options *vfs.MoveOptions) (bool, error) {
	c := s.getChild(path)
	if c == nil {
		return false, vfs.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path {
		return false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Move(ctx, c.NewPath, s.removePrefix(dest, c.Name), options)
}

type ChildResponse struct {
	Child   vfs.FileSystem
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
