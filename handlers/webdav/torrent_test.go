package webdav

import (
	"context"
	"strings"
	"testing"
	"time"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

// fakeTorrentAPI is a faithful stand-in for rest-api's list endpoint: it holds
// a flat set of files and answers a tree listing with the direct children of
// the requested path, exactly like services.List.buildTree. Every list call is
// recorded so tests can assert which directories were queried (and that the
// whole torrent was never flat-scanned).
type fakeTorrentAPI struct {
	files []ra.ListItem // type=file, PathStr like "/Top/dir/a.mkv"
	calls []api.ListResourceContentArgs
}

func (f *fakeTorrentAPI) ListResourceContentCached(_ context.Context, _ *api.Claims, _ string, args *api.ListResourceContentArgs) (*ra.ListResponse, error) {
	f.calls = append(f.calls, *args)

	// Normalise the requested path into components, mirroring rest-api which
	// trims surrounding slashes.
	trimmed := strings.Trim(args.Path, "/")
	var base []string
	if trimmed != "" {
		base = strings.Split(trimmed, "/")
	}

	seenDir := map[string]struct{}{}
	var items []ra.ListItem
	for _, file := range f.files {
		comps := strings.Split(strings.TrimPrefix(file.PathStr, "/"), "/")
		if !hasPrefixComps(comps, base) {
			continue
		}
		if len(comps) == len(base)+1 {
			// direct child file
			items = append(items, file)
			continue
		}
		// direct child directory
		dirComps := comps[:len(base)+1]
		dirPath := "/" + strings.Join(dirComps, "/")
		if _, ok := seenDir[dirPath]; ok {
			continue
		}
		seenDir[dirPath] = struct{}{}
		items = append(items, ra.ListItem{PathStr: dirPath, Type: ra.ListTypeDirectory})
	}

	return &ra.ListResponse{Items: items, Count: len(items)}, nil
}

func (f *fakeTorrentAPI) ExportResourceContent(_ context.Context, _ *api.Claims, _ string, _ string, _ string) (*ra.ExportResponse, error) {
	return &ra.ExportResponse{}, nil
}

func hasPrefixComps(comps, prefix []string) bool {
	if len(comps) < len(prefix) {
		return false
	}
	for i := range prefix {
		if comps[i] != prefix[i] {
			return false
		}
	}
	return true
}

func testCtx() context.Context {
	wc := &web.Context{ApiClaims: &api.Claims{}}
	return context.WithValue(context.Background(), web.Context{}, wc)
}

func file(path string, size int64, mime string) ra.ListItem {
	return ra.ListItem{PathStr: path, Type: ra.ListTypeFile, Size: size, MimeType: mime}
}

// A torrent whose files are all wrapped in a single top-level directory "Top"
// (the common case the prefix logic strips), with a few nesting levels.
func wrappedTorrent() *fakeTorrentAPI {
	return &fakeTorrentAPI{files: []ra.ListItem{
		file("/Top/movie.mkv", 100, "video/x-matroska"),
		file("/Top/subs/eng.srt", 5, "text/plain"),
		file("/Top/subs/rus.srt", 6, "text/plain"),
	}}
}

func newTorrentDir(f *fakeTorrentAPI) (*TorrentDirectory, *models.TorrentResource) {
	return &TorrentDirectory{api: f}, &models.TorrentResource{
		ResourceID: "hash",
		CreatedAt:  time.Unix(1700000000, 0),
	}
}

func TestParentPath(t *testing.T) {
	cases := map[string]string{
		"/Top/subs/eng.srt": "/Top/subs",
		"/Top/movie.mkv":    "/Top",
		"/Top":              "/",
		"/movie.mkv":        "/",
		"/Top/subs/":        "/Top",
	}
	for in, want := range cases {
		if got := parentPath(in); got != want {
			t.Errorf("parentPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Stat of a deep file must list only its parent directory (tree output), never
// flat-scan the whole torrent, and must resolve the item with its real
// metadata (prefix stripped).
func TestStat_ListsOnlyParentDir(t *testing.T) {
	f := wrappedTorrent()
	td, tr := newTorrentDir(f)

	fi, err := td.Stat(testCtx(), tr, "/subs/eng.srt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi == nil {
		t.Fatal("Stat returned nil for an existing file")
	}
	if fi.Path != "/subs/eng.srt" {
		t.Errorf("path = %q, want /subs/eng.srt (prefix must be stripped)", fi.Path)
	}
	if fi.IsDir || fi.Size != 5 || fi.MIMEType != "text/plain" {
		t.Errorf("unexpected file info: %+v", fi)
	}

	// Every list call must be a scoped tree listing with the max page size...
	for _, c := range f.calls {
		if c.Output != api.OutputTree {
			t.Errorf("expected only tree listings, got output=%q (path=%q)", c.Output, c.Path)
		}
		if c.Limit != 1000 {
			t.Errorf("expected Limit=1000, got %d", c.Limit)
		}
	}
	// ...and the file itself must have been resolved by listing its parent
	// directory "/Top/subs", not the torrent root or a flat list.
	if !calledWithPath(f, "/Top/subs") {
		t.Errorf("expected a listing of the parent dir /Top/subs, calls=%v", paths(f))
	}
	for _, c := range f.calls {
		if c.Path == "" {
			t.Errorf("a list call used an empty path (flat full-torrent scan): %v", paths(f))
		}
	}
}

// Open of a file resolves the same way and yields a content URL.
func TestOpen_ResolvesViaParentDir(t *testing.T) {
	f := wrappedTorrent()
	td, tr := newTorrentDir(f)

	_, u, err := td.Open(testCtx(), tr, "/movie.mkv")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = u
	if !calledWithPath(f, "/Top") {
		t.Errorf("expected Open to list parent dir /Top, calls=%v", paths(f))
	}
}

// ReadDir of a subdirectory lists exactly that directory and strips the prefix
// from returned paths.
func TestReadDir_ListsRequestedDir(t *testing.T) {
	f := wrappedTorrent()
	td, tr := newTorrentDir(f)

	fis, err := td.ReadDir(testCtx(), tr, "/subs", false)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(fis) != 2 {
		t.Fatalf("expected 2 entries in /subs, got %d: %+v", len(fis), fis)
	}
	for _, fi := range fis {
		if !strings.HasPrefix(fi.Path, "/subs/") {
			t.Errorf("entry path %q should be under /subs/ with prefix stripped", fi.Path)
		}
	}
	if !calledWithPath(f, "/Top/subs") {
		t.Errorf("expected listing of /Top/subs, calls=%v", paths(f))
	}
}

// A missing file yields a 404 (not a server error) and is resolved by listing
// only its parent directory — never a flat scan of the whole torrent.
func TestStat_MissingFile(t *testing.T) {
	f := wrappedTorrent()
	td, tr := newTorrentDir(f)

	fi, err := td.Stat(testCtx(), tr, "/subs/missing.srt")
	if fi != nil {
		t.Fatalf("expected nil FileInfo for a missing file, got %+v", fi)
	}
	if err == nil {
		t.Fatal("expected a 404 error for a missing file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error, got %v", err)
	}
	for _, c := range f.calls {
		if c.Path == "" {
			t.Errorf("missing file triggered a flat full-torrent scan: %v", paths(f))
		}
	}
}

func calledWithPath(f *fakeTorrentAPI, p string) bool {
	for _, c := range f.calls {
		if c.Path == p {
			return true
		}
	}
	return false
}

func paths(f *fakeTorrentAPI) []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Path)
	}
	return out
}
