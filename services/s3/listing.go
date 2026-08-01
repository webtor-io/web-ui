package s3

import (
	"context"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/vfs"
)

// entry is one listable thing: an object or, when a delimiter collapses a
// subtree, a common prefix. Keys are bucket-relative and never start with "/";
// prefixes always end with one.
type entry struct {
	Key      string
	IsDir    bool
	Size     int64
	ModTime  time.Time
	MIMEType string
	ETag     string
}

type listRequest struct {
	Bucket     string
	Prefix     string
	Delimiter  string
	MaxKeys    int
	StartAfter string
	V2         bool
	Token      string
	EncodeURL  bool
}

type listResult struct {
	Objects   []entry
	Prefixes  []string
	Truncated bool
	NextToken string
}

// list answers a ListObjects request against the virtual filesystem.
//
// The fast path is delimiter="/", which is what every interactive client sends
// when it browses a folder: it maps onto a single ReadDir. Anything else needs a
// recursive walk of the subtree — `rclone sync`, `--fast-list` and
// `aws s3 ls --recursive` all take that path, and it is the expensive one, so it
// is cached and capped (see walk).
func (h *Handler) list(ctx context.Context, req *listRequest) (*listResult, *Error) {
	if req.MaxKeys <= 0 {
		return &listResult{}, nil
	}
	base, namePrefix := splitPrefix(req.Prefix)

	var entries []entry
	var err error
	if req.Delimiter == "/" {
		entries, err = h.readDir(ctx, req.Bucket, base)
	} else {
		entries, err = h.walkCached(ctx, req.Bucket, base)
	}
	if err != nil {
		// A missing base directory is an empty listing in S3, not an error:
		// clients probe prefixes that do not exist all the time.
		if vfs.IsNotFound(err) {
			return &listResult{}, nil
		}
		return nil, errorFromVFS(err, false)
	}

	items := make([]entry, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Key, base+namePrefix) {
			continue
		}
		items = append(items, e)
	}

	// A non-"/" delimiter is rare but legal: collapse anything after the first
	// occurrence past the prefix, exactly like S3 does.
	if req.Delimiter != "" && req.Delimiter != "/" {
		items = collapse(items, req.Prefix, req.Delimiter)
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	after := req.StartAfter
	if req.Token != "" {
		t, err := decodeToken(req.Token)
		if err != nil {
			return nil, newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Invalid continuation token", err)
		}
		after = t
	}

	res := &listResult{}
	count := 0
	for _, it := range items {
		if after != "" && it.Key <= after {
			continue
		}
		if count == req.MaxKeys {
			res.Truncated = true
			res.NextToken = encodeToken(lastKey(res))
			break
		}
		if it.IsDir {
			res.Prefixes = append(res.Prefixes, it.Key)
		} else {
			res.Objects = append(res.Objects, it)
		}
		count++
	}
	return res, nil
}

// lastKey returns the greatest key emitted so far. Objects and prefixes are
// interleaved in one sorted stream, so paging has to resume from whichever of
// the two ended up last.
func lastKey(r *listResult) string {
	var k string
	if len(r.Objects) > 0 {
		k = r.Objects[len(r.Objects)-1].Key
	}
	if len(r.Prefixes) > 0 && r.Prefixes[len(r.Prefixes)-1] > k {
		k = r.Prefixes[len(r.Prefixes)-1]
	}
	return k
}

// readDir lists one level of the tree and converts it to bucket-relative keys.
func (h *Handler) readDir(ctx context.Context, bucket string, dir string) ([]entry, error) {
	fis, err := h.FileSystem.ReadDir(ctx, vfsPath(bucket, dir), false)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(fis))
	for i := range fis {
		entries = append(entries, toEntry(&fis[i], bucket))
	}
	return entries, nil
}

// walkCached is walk plus a short-lived per-user cache.
//
// One recursive listing costs one ReadDir per directory, i.e. one rest-api call
// per torrent, so a `rclone sync` of a large library would otherwise re-issue
// hundreds of calls for every page it fetches.
func (h *Handler) walkCached(ctx context.Context, bucket string, dir string) ([]entry, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	key := strings.Join([]string{userID, bucket, dir}, "|")
	return h.walks.Get(key, func() ([]entry, error) {
		return h.walk(ctx, bucket, dir)
	})
}

func (h *Handler) walk(ctx context.Context, bucket string, dir string) ([]entry, error) {
	var out []entry
	dirs := 0

	var rec func(d string) error
	rec = func(d string) error {
		if dirs >= h.MaxWalkDirs {
			return errListingTooLarge(bucket, d)
		}
		dirs++
		fis, err := h.FileSystem.ReadDir(ctx, vfsPath(bucket, d), false)
		if err != nil {
			// Only the root of the walk is allowed to fail the whole request;
			// a subtree that disappeared mid-walk (torrent removed, listing
			// expired) must not blank out everything already collected.
			if d != dir && vfs.IsNotFound(err) {
				log.WithField("bucket", bucket).WithField("dir", d).Warn("skipping vanished directory while walking")
				return nil
			}
			return err
		}
		for i := range fis {
			e := toEntry(&fis[i], bucket)
			if e.IsDir {
				if err := rec(e.Key); err != nil {
					return err
				}
				continue
			}
			if len(out) >= h.MaxWalkKeys {
				return errListingTooLarge(bucket, d)
			}
			out = append(out, e)
		}
		return nil
	}

	if err := rec(dir); err != nil {
		return nil, err
	}
	return out, nil
}

// errListingTooLarge fails loudly instead of returning a short listing that
// looks complete. A truncated-but-successful answer would make `rclone sync`
// delete everything it did not see.
func errListingTooLarge(bucket string, dir string) error {
	log.WithField("bucket", bucket).WithField("dir", dir).Error("recursive listing exceeded cap")
	return newError(http.StatusBadRequest, ErrCodeInvalidRequest,
		"This prefix is too large to list recursively — list it folder by folder (delimiter=/)",
		errors.Errorf("listing cap exceeded at %s/%s", bucket, dir))
}

// collapse implements delimiter grouping for delimiters other than "/".
func collapse(items []entry, prefix string, delim string) []entry {
	var out []entry
	seen := map[string]bool{}
	for _, it := range items {
		rest := strings.TrimPrefix(it.Key, prefix)
		idx := strings.Index(rest, delim)
		if idx < 0 {
			out = append(out, it)
			continue
		}
		cp := prefix + rest[:idx+len(delim)]
		if seen[cp] {
			continue
		}
		seen[cp] = true
		out = append(out, entry{Key: cp, IsDir: true})
	}
	return out
}

func toEntry(fi *vfs.FileInfo, bucket string) entry {
	key := strings.TrimPrefix(fi.Path, "/")
	key = strings.TrimPrefix(key, bucket+"/")
	return entry{
		Key:      key,
		IsDir:    fi.IsDir,
		Size:     fi.Size,
		ModTime:  fi.ModTime,
		MIMEType: fi.MIMEType,
		ETag:     fi.ETag,
	}
}

// splitPrefix cuts a request prefix into the directory to read and the leading
// characters of the names inside it ("Movies/Big" -> "Movies/", "Big").
func splitPrefix(prefix string) (string, string) {
	i := strings.LastIndex(prefix, "/")
	if i < 0 {
		return "", prefix
	}
	return prefix[:i+1], prefix[i+1:]
}

// hasDotSegment reports whether a key contains a "." or ".." path segment.
//
// S3 itself would treat those as ordinary characters in a key, but here keys
// are concatenated into a filesystem path, so refusing them keeps the concat in
// vfsPath from ever meaning something other than what it says. The tree below
// resolves names through the database rather than the filesystem, so this is
// belt-and-braces rather than a hole being closed — which is exactly why it is
// cheap to keep.
func hasDotSegment(key string) bool {
	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}

// vfsPath maps bucket + key onto a path in the shared library tree. Buckets are
// the tree's top-level directories, so this is a pure concatenation — that is
// the whole point of exposing the folders as buckets.
func vfsPath(bucket string, key string) string {
	if key == "" {
		return "/" + bucket + "/"
	}
	return "/" + bucket + "/" + key
}

func encodeToken(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

func decodeToken(token string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode continuation token")
	}
	return string(b), nil
}
