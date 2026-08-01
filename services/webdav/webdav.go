// Package webdav provides a client and server WebDAV filesystem implementation.
//
// WebDAV is defined in RFC 4918.
//
// The filesystem itself is protocol-neutral and lives in services/vfs (types)
// and handlers/vfs (the tree); the names below are aliases so that this package
// and its callers keep reading as WebDAV code.
package webdav

import (
	"github.com/webtor-io/web-ui/services/vfs"
)

// FileInfo holds information about a WebDAV file.
type FileInfo = vfs.FileInfo

type CreateOptions = vfs.CreateOptions

type RemoveAllOptions = vfs.RemoveAllOptions

type CopyOptions = vfs.CopyOptions

type MoveOptions = vfs.MoveOptions

// ConditionalMatch represents the value of a conditional header
// according to RFC 2068 section 14.25 and RFC 2068 section 14.26
// The (optional) value can either be a wildcard or an ETag.
type ConditionalMatch = vfs.ConditionalMatch
