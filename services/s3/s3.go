// Package s3 serves the shared library filesystem (services/libfs) over the S3
// REST API, so users can point rclone, the aws CLI, Cyberduck, s3fs or any other
// S3 client at their library.
//
// It is a sibling of services/webdav: same tree, same folder layout, different
// wire format. The four top-level folders are exposed as buckets, which keeps
// bucket + key a plain concatenation into a filesystem path.
//
// Read-only by design except for the `torrents` bucket, which mirrors what
// WebDAV allows there (upload a .torrent to add it to the library, delete to
// remove it, copy to rename).
package s3

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/vfs"
)

const (
	defaultMaxKeys = 1000
	// maxInlineObject bounds what we are willing to buffer for an inline
	// response. Only .torrent files take that path; anything bigger means the
	// filesystem handed us something it should have redirected for.
	maxInlineObject = 32 << 20
	// DefaultRegion is echoed to clients that ask where the bucket lives, and is
	// what the profile page suggests. Any region in a credential scope is
	// accepted — we verify with whatever the client chose — but a client that
	// asks has to be told something.
	DefaultRegion = "us-east-1"
)

// Handler serves S3 requests against a virtual filesystem.
type Handler struct {
	FileSystem vfs.FileSystem
	// SigningSecret derives every user's secret access key (DeriveSecretKey).
	SigningSecret string
	// MountPath is the URL prefix the API is published at ("/s3"), i.e. the
	// endpoint users configure. It is stripped before a request path is read as
	// bucket + key.
	MountPath string
	// MaxWalkDirs/MaxWalkKeys bound a recursive listing.
	MaxWalkDirs int
	MaxWalkKeys int
	// Now is overridable in tests; signature verification needs a clock.
	Now func() time.Time

	walks *lazymap.LazyMap[[]entry]
}

func New(fs vfs.FileSystem, signingSecret string, mountPath string) *Handler {
	return &Handler{
		FileSystem:    fs,
		SigningSecret: signingSecret,
		MountPath:     strings.TrimSuffix(mountPath, "/"),
		MaxWalkDirs:   2000,
		MaxWalkKeys:   50000,
		Now:           time.Now,
		// Each cached walk holds every key under a prefix, so the cache is sized
		// against memory, not hit rate: worst case is Capacity × MaxWalkKeys
		// entries resident. web-ui has been OOM-killed by unbounded caches
		// before — keep this deliberately small and short-lived.
		walks: lazymap.New[[]entry](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 10 * time.Second,
			Capacity:    32,
		}),
	}
}

type userContextKey struct{}

// WithUser tags a request context with the owning user, which is all the S3
// layer needs to know about accounts — it keys its listing cache by it.
func WithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

func userIDFromContext(ctx context.Context) (string, error) {
	id, ok := ctx.Value(userContextKey{}).(string)
	if !ok || id == "" {
		return "", errors.New("s3 user is not set in context")
	}
	return id, nil
}

// ServeHTTP handles one S3 request.
//
// r.URL must be the URI as the client sent it (path *and* query): the signature
// covers both, and the middleware chain in front of us rewrites them.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.serve(w, r); err != nil {
		h.writeError(w, r, err)
	}
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) *Error {
	if _, err := verifyRequest(r, r.URL, h.SigningSecret, h.now()); err != nil {
		return err
	}

	bucket, key := h.splitPath(r.URL)
	query := r.URL.Query()

	switch {
	case bucket == "":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return newError(http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "Method not allowed on the service root", nil)
		}
		return h.listBuckets(w, r)
	case key == "":
		return h.serveBucket(w, r, bucket, query)
	default:
		return h.serveObject(w, r, bucket, key, query)
	}
}

func (h *Handler) serveBucket(w http.ResponseWriter, r *http.Request, bucket string, query url.Values) *Error {
	if err := h.checkBucket(r.Context(), bucket); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodHead:
		w.Header().Set("x-amz-bucket-region", DefaultRegion)
		w.WriteHeader(http.StatusOK)
		return nil
	case http.MethodGet:
		if _, ok := query["location"]; ok {
			return h.writeXML(w, http.StatusOK, &locationConstraint{XMLNS: s3NS, Value: DefaultRegion})
		}
		for _, unsupported := range []string{"versioning", "policy", "acl", "cors", "lifecycle", "tagging", "uploads", "versions", "object-lock", "replication", "notification", "encryption", "accelerate", "logging", "website", "requestPayment"} {
			if _, ok := query[unsupported]; ok {
				return newError(http.StatusNotImplemented, ErrCodeNotImplemented, "This bucket sub-resource is not supported", nil)
			}
		}
		return h.listObjects(w, r, bucket, query)
	default:
		// Bucket creation/removal would mean creating library folders, which do
		// not exist as a concept: the four are fixed.
		return newError(http.StatusForbidden, ErrCodeAccessDenied, "Buckets are fixed and cannot be created, modified or deleted", nil)
	}
}

func (h *Handler) checkBucket(ctx context.Context, bucket string) *Error {
	fi, err := h.FileSystem.Stat(ctx, vfsPath(bucket, ""))
	if err != nil {
		return errorFromVFS(err, true)
	}
	if !fi.IsDir {
		return newError(http.StatusNotFound, ErrCodeNoSuchBucket, "The specified bucket does not exist", nil)
	}
	return nil
}

func (h *Handler) listBuckets(w http.ResponseWriter, r *http.Request) *Error {
	fis, err := h.FileSystem.ReadDir(r.Context(), "/", false)
	if err != nil {
		return errorFromVFS(err, true)
	}
	res := &listAllMyBucketsResult{XMLNS: s3NS}
	res.Owner = h.owner(r.Context())
	for _, fi := range fis {
		if !fi.IsDir {
			continue
		}
		res.Buckets.Bucket = append(res.Buckets.Bucket, bucket{
			Name:         strings.Trim(strings.TrimPrefix(fi.Path, "/"), "/"),
			CreationDate: formatListTime(fi.ModTime),
		})
	}
	return h.writeXML(w, http.StatusOK, res)
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request, bucketName string, query url.Values) *Error {
	req := &listRequest{
		Bucket:     bucketName,
		Delimiter:  query.Get("delimiter"),
		MaxKeys:    defaultMaxKeys,
		V2:         query.Get("list-type") == "2",
		EncodeURL:  strings.EqualFold(query.Get("encoding-type"), "url"),
		StartAfter: query.Get("start-after"),
		Token:      query.Get("continuation-token"),
		Prefix:     query.Get("prefix"),
	}
	if !req.V2 {
		req.StartAfter = query.Get("marker")
		req.Token = ""
	}
	if v := query.Get("max-keys"); v != "" {
		mk, err := strconv.Atoi(v)
		if err != nil || mk < 0 {
			return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Invalid max-keys", err)
		}
		req.MaxKeys = min(mk, defaultMaxKeys)
	}
	// encoding-type applies to the *response*. Request parameters arrive
	// encoded once, like every query parameter, and net/url has already decoded
	// them by the time we read them — decoding again would turn a literal "+"
	// inside a name into a space and the prefix would match nothing.

	if hasDotSegment(req.Prefix) {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Invalid prefix", nil)
	}

	res, err := h.list(r.Context(), req)
	if err != nil {
		return err
	}

	out := &listBucketResult{
		XMLNS:       s3NS,
		Name:        bucketName,
		Prefix:      encodeListValue(req.Prefix, req.EncodeURL),
		Delimiter:   encodeListValue(req.Delimiter, req.EncodeURL),
		MaxKeys:     req.MaxKeys,
		IsTruncated: res.Truncated,
	}
	if req.EncodeURL {
		out.EncodingType = "url"
	}
	for _, o := range res.Objects {
		out.Contents = append(out.Contents, object{
			Key:          encodeListValue(o.Key, req.EncodeURL),
			LastModified: formatListTime(o.ModTime),
			ETag:         etagFor(&o),
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}
	for _, p := range res.Prefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, commonPrefix{Prefix: encodeListValue(p, req.EncodeURL)})
	}
	if req.V2 {
		keyCount := len(out.Contents) + len(out.CommonPrefixes)
		out.KeyCount = &keyCount
		out.ContinuationToken = req.Token
		// Echo what the client sent, in its own encoding.
		out.StartAfter = encodeListValue(req.StartAfter, req.EncodeURL)
		if res.Truncated {
			out.NextContinuationToken = res.NextToken
		}
	} else {
		out.Marker = encodeListValue(req.StartAfter, req.EncodeURL)
		// NextMarker is only meaningful with a delimiter; without one the client
		// pages by the last key it received (RFC-less, but universal S3
		// behaviour).
		if res.Truncated && req.Delimiter != "" {
			out.NextMarker = encodeListValue(lastKey(res), req.EncodeURL)
		}
	}
	return h.writeXML(w, http.StatusOK, out)
}

func (h *Handler) serveObject(w http.ResponseWriter, r *http.Request, bucketName string, key string, query url.Values) *Error {
	if hasDotSegment(key) {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Invalid key", nil)
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return h.getObject(w, r, bucketName, key)
	case http.MethodPut:
		if src := r.Header.Get("x-amz-copy-source"); src != "" {
			return h.copyObject(w, r, bucketName, key, src)
		}
		return h.putObject(w, r, bucketName, key)
	case http.MethodDelete:
		return h.deleteObject(w, r, bucketName, key)
	case http.MethodPost:
		return newError(http.StatusNotImplemented, ErrCodeNotImplemented, "Multipart and POST uploads are not supported", nil)
	default:
		return newError(http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "Method not allowed", nil)
	}
}

func (h *Handler) getObject(w http.ResponseWriter, r *http.Request, bucketName string, key string) *Error {
	fi, err := h.FileSystem.Stat(r.Context(), vfsPath(bucketName, key))
	if err != nil {
		return errorFromVFS(err, false)
	}
	if fi.IsDir {
		// There are no directories in S3. A client that asks for one is
		// probing, and expects a miss rather than a folder.
		return newError(http.StatusNotFound, ErrCodeNoSuchKey, "The specified key does not exist", nil)
	}

	e := toEntry(fi, bucketName)
	if r.Method == http.MethodHead {
		// HEAD is answered from metadata alone. Opening would ask the streaming
		// chain to mint a content URL, and clients HEAD constantly (rclone
		// checks every object before transferring it).
		h.setObjectHeaders(w, &e)
		w.WriteHeader(http.StatusOK)
		return nil
	}

	body, redirect, err := h.FileSystem.Open(r.Context(), vfsPath(bucketName, key))
	if err != nil {
		return errorFromVFS(err, false)
	}
	if body != nil {
		defer func() {
			_ = body.Close()
		}()
	}

	// Torrent content is served by the streaming chain, not by us: hand the
	// client a redirect rather than proxying gigabytes through web-ui. Every S3
	// client we support follows it (the target URL carries its own auth, so the
	// dropped Authorization header on cross-host redirect is fine).
	if redirect != nil {
		if redirect.String() == "" {
			return newError(http.StatusInternalServerError, ErrCodeInternalError, "Failed to resolve the content location", nil)
		}
		http.Redirect(w, r, redirect.String(), http.StatusTemporaryRedirect)
		return nil
	}
	if body == nil {
		return newError(http.StatusInternalServerError, ErrCodeInternalError, "Failed to open the object", nil)
	}

	// The only objects served inline are .torrent files, which are small and
	// already in memory upstream. Buffering them lets ServeContent answer Range
	// and conditional requests properly instead of forcing a bare 200 on a
	// client that asked for a range.
	buf, err := io.ReadAll(io.LimitReader(body, maxInlineObject+1))
	if err != nil {
		return newError(http.StatusInternalServerError, ErrCodeInternalError, "Failed to read the object", err)
	}
	if int64(len(buf)) > maxInlineObject {
		return newError(http.StatusInternalServerError, ErrCodeInternalError, "Object is too large to serve inline", nil)
	}
	// Content-Length, Last-Modified and Accept-Ranges are ServeContent's job
	// here — it has to be free to answer 206 with its own length.
	w.Header().Set("ETag", etagFor(&e))
	if e.MIMEType != "" {
		w.Header().Set("Content-Type", e.MIMEType)
	}
	http.ServeContent(w, r, key, e.ModTime, bytes.NewReader(buf))
	return nil
}

func (h *Handler) putObject(w http.ResponseWriter, r *http.Request, bucketName string, key string) *Error {
	body, cerr := decodeBody(r)
	if cerr != nil {
		return cerr
	}
	defer func() {
		_ = body.Close()
	}()
	fi, _, err := h.FileSystem.Create(r.Context(), vfsPath(bucketName, key), body, &vfs.CreateOptions{
		IfMatch:     vfs.ConditionalMatch(r.Header.Get("If-Match")),
		IfNoneMatch: vfs.ConditionalMatch(r.Header.Get("If-None-Match")),
	})
	if err != nil {
		return errorFromVFS(err, false)
	}
	e := toEntry(fi, bucketName)
	w.Header().Set("ETag", etagFor(&e))
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *Handler) copyObject(w http.ResponseWriter, r *http.Request, bucketName string, key string, source string) *Error {
	src, err := url.PathUnescape(strings.TrimPrefix(source, "/"))
	if err != nil {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Malformed x-amz-copy-source", err)
	}
	srcBucket, srcKey, _ := strings.Cut(src, "/")
	if srcBucket != bucketName {
		return newError(http.StatusForbidden, ErrCodeAccessDenied, "Copying between buckets is not supported", nil)
	}
	// S3 has no rename: clients do copy-then-delete. The filesystem only offers
	// Move, so a copy onto a different key is a rename and a copy onto itself
	// (metadata-only copy) is a no-op the client just wants acknowledged.
	if srcKey != key {
		if _, err := h.FileSystem.Move(r.Context(), vfsPath(bucketName, srcKey), vfsPath(bucketName, key), &vfs.MoveOptions{}); err != nil {
			return errorFromVFS(err, false)
		}
	}
	fi, ferr := h.FileSystem.Stat(r.Context(), vfsPath(bucketName, key))
	if ferr != nil {
		return errorFromVFS(ferr, false)
	}
	e := toEntry(fi, bucketName)
	return h.writeXML(w, http.StatusOK, &copyObjectResult{
		XMLNS:        s3NS,
		LastModified: formatListTime(fi.ModTime),
		ETag:         etagFor(&e),
	})
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request, bucketName string, key string) *Error {
	err := h.FileSystem.RemoveAll(r.Context(), vfsPath(bucketName, key), &vfs.RemoveAllOptions{})
	// DeleteObject is idempotent in S3: a missing key is still a 204. Clients
	// rely on it — a move is copy-then-delete, and our copy already moved the
	// source away, so the delete that follows must not fail the operation.
	if err != nil && !vfs.IsNotFound(err) {
		return errorFromVFS(err, false)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) setObjectHeaders(w http.ResponseWriter, e *entry) {
	w.Header().Set("Content-Length", strconv.FormatInt(e.Size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", etagFor(e))
	if e.MIMEType != "" {
		w.Header().Set("Content-Type", e.MIMEType)
	}
	if !e.ModTime.IsZero() {
		w.Header().Set("Last-Modified", e.ModTime.UTC().Format(http.TimeFormat))
	}
}

// etagFor produces a stable, deliberately non-MD5 ETag.
//
// It must never be 32 hex characters: rclone (and others) treat a 32-hex ETag as
// the object's MD5 and verify downloads against it, which would fail every
// transfer since we never hash the content. A 40-char sha1 of the identity fails
// that pattern and is read as "no checksum available".
func etagFor(e *entry) string {
	if e.ETag != "" {
		return strconv.Quote(e.ETag)
	}
	sum := sha1.Sum([]byte(e.Key + "|" + strconv.FormatInt(e.Size, 10) + "|" + e.ModTime.UTC().Format(time.RFC3339)))
	return strconv.Quote(hex.EncodeToString(sum[:]))
}

func (h *Handler) owner(ctx context.Context) owner {
	id, err := userIDFromContext(ctx)
	if err != nil {
		id = "webtor"
	}
	return owner{ID: id, DisplayName: "webtor"}
}

// splitPath turns the request URI into bucket + key, path-style. Virtual-host
// style (bucket.endpoint) would need wildcard DNS and is not supported; clients
// must use force_path_style, which is the default for non-AWS endpoints.
func (h *Handler) splitPath(u *url.URL) (string, string) {
	p := strings.TrimPrefix(u.Path, h.MountPath)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", ""
	}
	bucketName, key, _ := strings.Cut(p, "/")
	return bucketName, key
}

func (h *Handler) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

func (h *Handler) writeXML(w http.ResponseWriter, status int, body any) *Error {
	buf, err := xml.Marshal(body)
	if err != nil {
		return newError(http.StatusInternalServerError, ErrCodeInternalError, "Failed to encode response", err)
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(xml.Header)+len(buf)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(buf)
	return nil
}

// WriteError renders an S3 error document. Exported because the gin layer
// rejects unauthenticated requests before this handler is reached, and an S3
// client shown a bare 403 with an empty body reports something unhelpful.
func WriteError(w http.ResponseWriter, r *http.Request, e *Error) {
	writeError(w, r, e)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, e *Error) {
	writeError(w, r, e)
}

func writeError(w http.ResponseWriter, r *http.Request, e *Error) {
	entry := log.WithError(e).WithField("path", r.URL.Path).WithField("method", r.Method)
	switch {
	case e.Status >= http.StatusInternalServerError:
		entry.Error("s3 request failed")
	case e.Status == http.StatusNotFound:
		// A miss is the normal way an S3 client asks "does this exist?" — rclone
		// HEADs every object before touching it. Logging those at warn level
		// would drown the errors that matter.
		entry.Debug("s3 object not found")
	default:
		entry.Warn("s3 request rejected")
	}
	body, err := xml.Marshal(&errorResponse{
		Code:             e.Code,
		Message:          e.Message,
		Resource:         r.URL.Path,
		CanonicalRequest: e.CanonicalRequest,
		StringToSign:     e.StringToSign,
	})
	if err != nil {
		http.Error(w, e.Message, e.Status)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(xml.Header)+len(body)))
	w.WriteHeader(e.Status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(body)
}

// encodeListValue applies encoding-type=url when the client asked for it.
//
// Percent-encoding, the way Amazon does it — NOT url.QueryEscape. The two
// differ on the space: QueryEscape writes "+", which only decodes back to a
// space in a form decoder. Clients that percent-decode (Cyberduck among them)
// would render "+" literally and then ask for a key nobody has. Slashes stay
// readable; everything else, "+" included, goes out as %XX.
func encodeListValue(v string, encode bool) string {
	if !encode || v == "" {
		return v
	}
	return uriEncode(v, false)
}
