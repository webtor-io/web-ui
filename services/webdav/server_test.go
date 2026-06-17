package webdav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeFS is a minimal read-only FileSystem whose every path is a directory
// carrying a fixed ModTime, enough to exercise PROPFIND on a collection.
type fakeFS struct{ modTime time.Time }

func (f fakeFS) Open(context.Context, string) (io.ReadCloser, *url.URL, error) {
	return nil, nil, NewHTTPError(http.StatusNotFound, nil)
}
func (f fakeFS) Stat(_ context.Context, name string) (*FileInfo, error) {
	return &FileInfo{Path: name, IsDir: true, ModTime: f.modTime}, nil
}
func (f fakeFS) ReadDir(_ context.Context, name string, _ bool) ([]FileInfo, error) {
	if name == "/" {
		return []FileInfo{{Path: "/all/", IsDir: true, ModTime: f.modTime}}, nil
	}
	return nil, nil
}
func (f fakeFS) Create(context.Context, string, io.ReadCloser, *CreateOptions) (*FileInfo, bool, error) {
	return nil, false, NewHTTPError(http.StatusForbidden, nil)
}
func (f fakeFS) RemoveAll(context.Context, string, *RemoveAllOptions) error {
	return NewHTTPError(http.StatusForbidden, nil)
}
func (f fakeFS) Mkdir(context.Context, string) error { return NewHTTPError(http.StatusForbidden, nil) }
func (f fakeFS) Copy(context.Context, string, string, *CopyOptions) (bool, error) {
	return false, NewHTTPError(http.StatusForbidden, nil)
}
func (f fakeFS) Move(context.Context, string, string, *MoveOptions) (bool, error) {
	return false, NewHTTPError(http.StatusForbidden, nil)
}

// rcloneOwncloudPropfind is the listing body rclone sends in owncloud/nextcloud
// mode: <displayname> is requested before <resourcetype>, and there are props
// (getcontentlength, oc:permissions) we cannot satisfy for a collection.
const rcloneOwncloudPropfind = `<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
 <d:prop>
  <d:displayname/>
  <d:getlastmodified/>
  <d:getcontentlength/>
  <d:resourcetype/>
  <oc:permissions/>
 </d:prop>
</d:propfind>`

func TestPropFind_DirectoryCarriesNameAndModTime(t *testing.T) {
	modTime := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	h := &Handler{FileSystem: fakeFS{modTime: modTime}}

	r := httptest.NewRequest("PROPFIND", "/", strings.NewReader(rcloneOwncloudPropfind))
	r.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// The directory must advertise its name and modification time...
	if !strings.Contains(body, "<displayname xmlns=\"DAV:\">all</displayname>") {
		t.Errorf("directory listing missing displayname:\n%s", body)
	}
	if !strings.Contains(body, "14 Mar 2026 09:30:00 GMT") {
		t.Errorf("directory listing missing getlastmodified:\n%s", body)
	}

	// ...and the 2xx propstat must precede the 404 one, or rclone in
	// owncloud/nextcloud mode (which only checks the first propstat's status)
	// discards the entry and the listing comes back empty.
	ok := strings.Index(body, "HTTP/1.1 200 OK")
	notFound := strings.Index(body, "HTTP/1.1 404 Not Found")
	if ok == -1 || notFound == -1 {
		t.Fatalf("expected both 200 and 404 propstats, got:\n%s", body)
	}
	if ok > notFound {
		t.Errorf("200 propstat must come before 404 propstat (rclone reads Status[0]):\n%s", body)
	}
}
