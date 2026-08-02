package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	docs "github.com/webtor-io/web-ui/docs/swagger"
	"github.com/webtor-io/web-ui/services/libapi"
)

//	@title			Webtor Account API
//	@version		1.0
//	@description	Programmatic access to Webtor resources, your library, your Vault and your account preferences.
//	@description
//	@description	## Two halves
//	@description
//	@description	`/resource`, `/list` and `/export` are the public Webtor API you already know: same paths, same
//	@description	parameters, same response bodies as [rest-api](https://api.webtor.io/), authenticated with your
//	@description	account key instead of an API key + secret. Code written against rest-api works here unchanged.
//	@description
//	@description	`/library`, `/vault` and `/profile` are account-scoped and exist only here. The library is the same
//	@description	one WebDAV and S3 serve, so a torrent added through this API shows up in a mounted drive
//	@description	immediately — it is one library, not a copy.
//	@description
//	@description	A typical flow: `POST /resource` with a magnet or `.torrent` → `POST /library` with the id it
//	@description	returns → `GET /resource/{id}/list` to see the files → `GET /resource/{id}/export/{file}` for the
//	@description	stream and download URLs.
//	@description
//	@description	## Authentication
//	@description
//	@description	Issue a key on your profile page and send it as `Authorization: Bearer <key>` (or `X-Api-Key: <key>`
//	@description	where a proxy strips `Authorization`). Rotating the key on the profile page revokes the old one at
//	@description	once.
//	@description
//	@description	The API is available on paid plans; a free account gets `402 payment_required`.
//	@description
//	@description	## Errors
//	@description
//	@description	Every failure answers `{"error": {"code": "...", "message": "..."}}`. Branch on `code`, not on the
//	@description	status: `unauthorized` (no or bad key), `forbidden` (wrong scope or plan), `payment_required` (free
//	@description	plan), `not_found`, `conflict`, `bad_request`, `upstream_error` / `upstream_timeout` (the services
//	@description	behind this one — often worth retrying), `unavailable`, `internal_error`.

//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				API key from your profile page, as `Bearer <key>`.

//	@tag.name			resource
//	@tag.description	Storing and reading torrents. Same contract as rest-api.
//	@tag.name			list
//	@tag.description	Files and directories inside a resource. Same contract as rest-api.
//	@tag.name			export
//	@tag.description	Stream and download URLs for a file. Same contract as rest-api.
//	@tag.name			library
//	@tag.description	The torrents saved to your account — the library WebDAV and S3 mount.
//	@tag.name			vault
//	@tag.description	Vault Points and the pledges that keep content available long-term.
//	@tag.name			profile
//	@tag.description	Account state and preferences.

// docsInstanceName must match the --instanceName the spec is generated with
// (see the swagger target in the Makefile).
//
// **It cannot be swaggo's default.** Every generated docs package registers
// itself globally by instance name at init, and rest-api's own spec — linked
// into this binary through services/api — already claims the default. Two
// packages under one name panic the process on startup, not at the first
// request. That is what the named instance buys.
const docsInstanceName = "libraryapi"

// registerDocs publishes the reference and the raw spec.
//
// Both are public: an API reference you need a key to read is a reference
// nobody evaluates before signing up. Nothing user-specific is in it.
func registerDocs(r *gin.Engine, mountPath string, endpoint string) {
	// The generated spec is host-agnostic; point it at whatever this
	// deployment actually serves (a dedicated api.<domain> or <domain>/api/v1)
	// so "Try it out" hits the right origin.
	if host, base, ok := splitEndpoint(endpoint); ok {
		docs.SwaggerInfolibraryapi.Host = host
		docs.SwaggerInfolibraryapi.BasePath = base
		docs.SwaggerInfolibraryapi.Schemes = []string{"https"}
		if strings.HasPrefix(endpoint, "http://") {
			docs.SwaggerInfolibraryapi.Schemes = []string{"http"}
		}
	}

	specPath := mountPath + "/swagger.json"
	r.GET(specPath, func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(docs.SwaggerInfolibraryapi.ReadDoc()))
	})
	r.GET(mountPath+"/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
		ginSwagger.URL(specPath), ginSwagger.InstanceName(docsInstanceName)))
}

// splitEndpoint cuts "https://api.example.com/v1" into host and base path.
func splitEndpoint(endpoint string) (host string, basePath string, ok bool) {
	e := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	if e == "" {
		return "", "", false
	}
	host, rest, found := strings.Cut(e, "/")
	if !found {
		return host, libapi.MountPath, true
	}
	return host, "/" + strings.TrimSuffix(rest, "/"), true
}
