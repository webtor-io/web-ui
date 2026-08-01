package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/s3"
	"github.com/webtor-io/web-ui/services/vfs"
)

const (
	key    = "99999999-8888-7777-6666-555555555555"
	secret = "signing-secret"
	user   = "11111111-2222-3333-4444-555555555555"
)

var modTime = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

type memFS struct {
	dirs  map[string]bool
	files map[string]int64
}

func newMemFS() *memFS {
	return &memFS{
		dirs: map[string]bool{
			"/": true, "/all/": true, "/movies/": true, "/series/": true, "/torrents/": true,
			"/all/Andor (Season 2) WEB-DL 1080p/": true,
		},
		files: map[string]int64{
			"/all/Andor (Season 2) WEB-DL 1080p/S02E01.mkv": 2147483648,
			"/all/Bugonia.2025.1080p.mkv":                   3221225472,
		},
	}
}

func (f *memFS) Stat(_ context.Context, name string) (*vfs.FileInfo, error) {
	if f.dirs[name] || f.dirs[name+"/"] {
		p := strings.TrimSuffix(name, "/") + "/"
		if name == "/" {
			p = "/"
		}
		return &vfs.FileInfo{Path: p, IsDir: true, ModTime: modTime}, nil
	}
	if size, ok := f.files[name]; ok {
		return &vfs.FileInfo{Path: name, Size: size, ModTime: modTime}, nil
	}
	return nil, vfs.NewHTTPError(http.StatusNotFound, nil)
}

func (f *memFS) ReadDir(_ context.Context, name string, _ bool) ([]vfs.FileInfo, error) {
	if !f.dirs[name] && !f.dirs[name+"/"] {
		return nil, vfs.NewHTTPError(http.StatusNotFound, nil)
	}
	dir := strings.TrimSuffix(name, "/") + "/"
	var out []vfs.FileInfo
	for d := range f.dirs {
		if d == dir || !strings.HasPrefix(d, dir) || strings.Count(strings.TrimPrefix(d, dir), "/") != 1 {
			continue
		}
		out = append(out, vfs.FileInfo{Path: d, IsDir: true, ModTime: modTime})
	}
	for p, size := range f.files {
		if !strings.HasPrefix(p, dir) || strings.Contains(strings.TrimPrefix(p, dir), "/") {
			continue
		}
		out = append(out, vfs.FileInfo{Path: p, Size: size, ModTime: modTime})
	}
	return out, nil
}

func (f *memFS) Open(_ context.Context, name string) (io.ReadCloser, *url.URL, error) {
	return io.NopCloser(strings.NewReader("body")), nil, nil
}
func (f *memFS) Create(context.Context, string, io.ReadCloser, *vfs.CreateOptions) (*vfs.FileInfo, bool, error) {
	return nil, false, vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *memFS) RemoveAll(context.Context, string, *vfs.RemoveAllOptions) error {
	return vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *memFS) Mkdir(context.Context, string) error {
	return vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *memFS) Copy(context.Context, string, string, *vfs.CopyOptions) (bool, error) {
	return false, vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *memFS) Move(context.Context, string, string, *vfs.MoveOptions) (bool, error) {
	return false, vfs.NewHTTPError(http.StatusForbidden, nil)
}

func main() {
	h := s3.New(newMemFS(), secret, "/s3")
	log.Infof("secret key: %s", s3.DeriveSecretKey(secret, key))
	srv := &http.Server{Addr: "127.0.0.1:9000", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(s3.WithUser(r.Context(), user))
		h.ServeHTTP(w, r)
	})}
	log.Fatal(srv.ListenAndServe())
}
