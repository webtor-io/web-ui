package webdav

import (
	"context"
	"io"
	"net/url"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/webdav"
)

type BaseDirectory struct{}

func (s *BaseDirectory) Create(ctx context.Context, name string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	return nil, false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Stat(ctx context.Context, name string) (*webdav.FileInfo, error) {
	return nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) ReadDir(ctx context.Context, name string, recursive bool) ([]webdav.FileInfo, error) {
	return nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Open(ctx context.Context, name string) (io.ReadCloser, *url.URL, error) {
	return nil, nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) RemoveAll(ctx context.Context, name string, opts *webdav.RemoveAllOptions) error {
	return webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Move(ctx context.Context, name, dest string, options *webdav.MoveOptions) (bool, error) {
	return false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}
func (s *BaseDirectory) Mkdir(ctx context.Context, name string) error {
	return webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

func (s *BaseDirectory) Copy(ctx context.Context, name, dest string, options *webdav.CopyOptions) (bool, error) {
	return false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
}

var _ webdav.FileSystem = (*BaseDirectory)(nil)
