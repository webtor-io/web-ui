package libfs

import (
	"context"
	"io"
	"net/url"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/vfs"
)

type BaseDirectory struct{}

func (s *BaseDirectory) Create(ctx context.Context, name string, body io.ReadCloser, opts *vfs.CreateOptions) (*vfs.FileInfo, bool, error) {
	return nil, false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Stat(ctx context.Context, name string) (*vfs.FileInfo, error) {
	return nil, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) ReadDir(ctx context.Context, name string, recursive bool) ([]vfs.FileInfo, error) {
	return nil, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Open(ctx context.Context, name string) (io.ReadCloser, *url.URL, error) {
	return nil, nil, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) RemoveAll(ctx context.Context, name string, opts *vfs.RemoveAllOptions) error {
	return vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Move(ctx context.Context, name, dest string, options *vfs.MoveOptions) (bool, error) {
	return false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}
func (s *BaseDirectory) Mkdir(ctx context.Context, name string) error {
	return vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Copy(ctx context.Context, name, dest string, options *vfs.CopyOptions) (bool, error) {
	return false, vfs.NewHTTPError(403, errors.New("operation not permitted"))
}

var _ vfs.FileSystem = (*BaseDirectory)(nil)
