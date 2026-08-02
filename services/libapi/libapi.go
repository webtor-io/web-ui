// Package libapi holds what the JSON API needs before any endpoint runs: where
// it is mounted, how a request's API key becomes an authenticated account, and
// the shapes of the account-scoped payloads (library, vault, profile).
//
// It deliberately does **not** model resources, listings or exports. Those
// endpoints are pass-throughs to rest-api and return its own types verbatim
// (see handlers/api) — a copy of those structs here would be one more thing to
// keep in sync, and having nothing to keep in sync is the point of a
// pass-through.
package libapi

import (
	"strings"

	"github.com/urfave/cli"
	co "github.com/webtor-io/web-ui/services/common"
)

const (
	// MountPath is the URL prefix the API is published at. It is versioned:
	// clients pin to it, and a breaking change gets /api/v2 rather than a
	// silent reinterpretation of the same paths.
	MountPath = "/api/v1"

	// HostPrefix is what a dedicated hostname's requests are prefixed with
	// (see RegisterHostMiddleware). It is MountPath without the version, so
	// api.<domain>/v1/... and <domain>/api/v1/... are the same route.
	HostPrefix = "/api"

	// Version is the path segment MountPath ends with, i.e. what a dedicated
	// host's URLs start with.
	Version = "v1"

	// TokenName is the access_token row the API key lives in; Scope* are what
	// it is issued with. Same row shape as the S3 and WebDAV credentials, so
	// issuing and rotating reuse access_token.Generate / Regenerate untouched.
	TokenName  = "api"
	ScopeRead  = "api:read"
	ScopeWrite = "api:write"
)

// PublicEndpoint is the base URL users put in their client: the dedicated host
// when there is one, otherwise the main domain plus the mount path. Both forms
// address the same routes, and the version is part of the path either way.
func PublicEndpoint(c *cli.Context) string {
	if hosts := Hosts(c); len(hosts) > 0 {
		return "https://" + hosts[0] + "/" + Version
	}
	return strings.TrimSuffix(c.String(co.DomainFlag), "/") + MountPath
}
