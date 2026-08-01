package libfs

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/vfs"
	"github.com/webtor-io/web-ui/services/web"
)

func newDirectoryFileInfo(path string) vfs.FileInfo {
	now := time.Now()
	return vfs.FileInfo{
		Path:    strings.TrimSuffix(path, "/") + "/",
		ModTime: now,
		IsDir:   true,
	}
}

func getWebContext(ctx context.Context) (*web.Context, error) {
	wc := ctx.Value(web.Context{})
	wcc, ok := wc.(*web.Context)
	if !ok {
		return nil, errors.New("web context is not set")
	}
	return wcc, nil
}

func isRoot(path string) bool {
	return path == "/"
}

func AddPrefix(fi *vfs.FileInfo, prefix string) *vfs.FileInfo {
	fi.Path = strings.TrimSuffix(prefix, "/") + "/" + strings.TrimPrefix(fi.Path, "/")
	return fi
}

func AddPrefixes(fis []vfs.FileInfo, prefix string) []vfs.FileInfo {
	for i, fi := range fis {
		fis[i].Path = AddPrefix(&fi, prefix).Path
	}
	return fis
}
