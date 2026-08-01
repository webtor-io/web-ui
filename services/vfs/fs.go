// Package vfs describes the read-mostly virtual filesystem that backs the
// user-facing library protocols.
//
// It is deliberately protocol-neutral: the tree in handlers/vfs implements this
// interface once (library folders, torrent contents, .torrent files) and each
// protocol layer — services/webdav (RFC 4918) and services/s3 (S3 REST) —
// adapts it to its own wire format. Nothing here may depend on a protocol
// package.
package vfs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

// FileInfo holds information about a file in the virtual filesystem.
type FileInfo struct {
	Path     string
	Size     int64
	ModTime  time.Time
	IsDir    bool
	MIMEType string
	ETag     string
}

// FileSystem is the backend implemented by the directory tree in handlers/vfs.
//
// Open returns either a body to stream (small, locally-available objects such
// as .torrent files) or a URL to redirect the client to (torrent content served
// by the streaming chain) — never both.
type FileSystem interface {
	Open(ctx context.Context, name string) (io.ReadCloser, *url.URL, error)
	Stat(ctx context.Context, name string) (*FileInfo, error)
	ReadDir(ctx context.Context, name string, recursive bool) ([]FileInfo, error)
	Create(ctx context.Context, name string, body io.ReadCloser, opts *CreateOptions) (fileInfo *FileInfo, created bool, err error)
	RemoveAll(ctx context.Context, name string, opts *RemoveAllOptions) error
	Mkdir(ctx context.Context, name string) error
	Copy(ctx context.Context, name, dest string, options *CopyOptions) (created bool, err error)
	Move(ctx context.Context, name, dest string, options *MoveOptions) (created bool, err error)
}

type CreateOptions struct {
	IfMatch     ConditionalMatch
	IfNoneMatch ConditionalMatch
}

type RemoveAllOptions struct {
	IfMatch     ConditionalMatch
	IfNoneMatch ConditionalMatch
}

type CopyOptions struct {
	NoRecursive bool
	NoOverwrite bool
}

type MoveOptions struct {
	NoOverwrite bool
}

// ConditionalMatch represents the value of a conditional header
// according to RFC 2068 section 14.25 and RFC 2068 section 14.26
// The (optional) value can either be a wildcard or an ETag.
type ConditionalMatch string

func (val ConditionalMatch) IsSet() bool {
	return val != ""
}

func (val ConditionalMatch) IsWildcard() bool {
	return val == "*"
}

func (val ConditionalMatch) ETag() (string, error) {
	s, err := strconv.Unquote(string(val))
	if err != nil {
		return "", errors.Wrapf(err, "failed to unquote etag %s", string(val))
	}
	return s, nil
}

func (val ConditionalMatch) MatchETag(etag string) (bool, error) {
	if etag == "" {
		return false, nil
	}
	if val.IsWildcard() {
		return true, nil
	}
	t, err := val.ETag()
	return t == etag, err
}

// HTTPError associates an error with an HTTP status code. Filesystem
// implementations return it to convey semantics (404 not found, 403 access
// denied, …) that every protocol layer maps onto its own error format.
type HTTPError struct {
	Code int
	Err  error
}

func (err *HTTPError) Error() string {
	s := fmt.Sprintf("%v %v", err.Code, http.StatusText(err.Code))
	if err.Err != nil {
		return fmt.Sprintf("%v: %v", s, err.Err)
	}
	return s
}

func (err *HTTPError) Unwrap() error {
	return err.Err
}

// NewHTTPError creates a new error that is associated with an HTTP status code
// and optionally an error that lead to it.
func NewHTTPError(statusCode int, cause error) error {
	return &HTTPError{Code: statusCode, Err: cause}
}

// HTTPErrorf is NewHTTPError with a formatted cause.
func HTTPErrorf(code int, format string, a ...interface{}) *HTTPError {
	return &HTTPError{Code: code, Err: fmt.Errorf(format, a...)}
}

// HTTPErrorFromError coerces any error into a *HTTPError, defaulting to 500.
func HTTPErrorFromError(err error) *HTTPError {
	if err == nil {
		return nil
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr
	}
	return &HTTPError{Code: http.StatusInternalServerError, Err: err}
}

// StatusCode returns the HTTP status a protocol layer should answer with.
func StatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	return HTTPErrorFromError(err).Code
}

func IsNotFound(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code == http.StatusNotFound
	}
	return false
}
