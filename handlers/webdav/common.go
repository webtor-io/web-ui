package webdav

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/web"
	"github.com/webtor-io/web-ui/services/webdav"
)

func newDirectoryFileInfo(path string) webdav.FileInfo {
	now := time.Now()
	return webdav.FileInfo{
		Path:    path,
		ModTime: now,
		IsDir:   true,
	}
}

func getWebContext(ctx context.Context) (*web.Context, error) {
	wc := ctx.Value(web.Context{})
	wcc, ok := wc.(*web.Context)
	if !ok {
		return nil, errors.New("webdav context is not set")
	}
	return wcc, nil
}

func isRoot(path string) bool {
	return path == "/"
}
