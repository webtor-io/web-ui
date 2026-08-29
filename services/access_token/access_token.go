package access_token

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	common2 "github.com/webtor-io/web-ui/services/common"
)

type AccessToken struct {
	pg *cs.PG
}

func New(pg *cs.PG) *AccessToken {
	return &AccessToken{
		pg: pg,
	}
}

func (s *AccessToken) Generate(c *gin.Context, name string, scope []string) (*models.AccessToken, error) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		return nil, fmt.Errorf("no auth")
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return models.MakeAccessToken(c.Request.Context(), db, u.ID, name, scope)
}

// Regenerate rotates the token value for an existing (user, name) pair.
// Destructive: any addon installed with the previous URL stops working.
func (s *AccessToken) Regenerate(c *gin.Context, name string, scope []string) (*models.AccessToken, error) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		return nil, fmt.Errorf("no auth")
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return models.RegenerateAccessToken(c.Request.Context(), db, u.ID, name, scope)
}

func (s *AccessToken) GetTokenByName(c *gin.Context, name string) (*models.AccessToken, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		return nil, fmt.Errorf("no auth")
	}
	return models.GetAccessTokenByName(c.Request.Context(), db, u.ID, name)
}

type TokenScope struct{}

// tokenAuthPrefixes are the mount points that authenticate by access token
// and verify the token's scope before acting.
//
// Deliberately a short, explicit list rather than anything derived: it is the
// contract that says which surfaces a bearer credential may speak to. Adding
// an entry means committing that everything under it checks scope.
//
// /api-credentials and /s3-credentials are NOT here and must not be — they
// hand out the credentials themselves, and they authenticate by session. Note
// they escape the list only because of the boundary check below; a bare
// strings.HasPrefix against "/api" would have swallowed /api-credentials.
var tokenAuthPrefixes = []string{
	"/api",
	"/s3",
	"/stremio",
	"/webdav",
}

// tokenAuthPath reports whether a path is under a mount that accepts token
// authentication. Matching is on path segment boundaries, so "/api" matches
// "/api/v1/library" but not "/api-credentials/key".
func tokenAuthPath(p string) bool {
	for _, prefix := range tokenAuthPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func (s *AccessToken) RegisterHandler(r *gin.Engine) {
	prefix := fmt.Sprintf("/%s/", common2.AccessTokenParamName)
	r.Match(common.AnyMethods, prefix+"*rest", func(c *gin.Context) {
		parts := strings.SplitN(strings.TrimPrefix(c.Param("rest"), "/"), "/", 2)
		if len(parts) < 2 {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		token := parts[0]
		rest := "/" + parts[1]

		query := c.Request.URL.Query()
		query.Set(common2.AccessTokenParamName, token)
		c.Request.URL.RawQuery = query.Encode()
		c.Request.URL.Path = rest
		c.Request.URL.RawPath = common2.EscapePath(rest)
		c.Abort()
		r.HandleContext(c)
	})
	r.Use(func(c *gin.Context) {
		if c.Query(common2.AccessTokenParamName) == "" {
			c.Next()
			return
		}
		// A token establishes identity only on the surfaces that go on to
		// verify its scope. This middleware is global, so without the check
		// a token minted for one purpose authenticated every route in the
		// app: a stremio:read token — a value that travels in a URL handed
		// to a third-party client — reached /profile/export, which returns
		// every other token of that account in cleartext, and
		// /profile/delete. The S3 access key id is the same value, and by
		// S3's own model an access key id is not a secret.
		//
		// Scope is checked per-surface (at.HasScope in handlers/webdav and
		// handlers/stremio, the equivalent in handlers/s3 and handlers/api);
		// what this gate adds is that everything NOT in that list gets no
		// identity from a token at all. New routes are therefore closed by
		// default, which is the direction a mistake should fall.
		if !tokenAuthPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		at, err := s.getToken(c.Request.Context(), c.Query(common2.AccessTokenParamName))
		if err != nil {
			// A malformed token is a bad request from this caller, not a
			// server fault: getToken fails on any non-UUID, so returning 500
			// here meant `/?token=x` answered 500 on every URL in the app.
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}
		if at != nil {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, at.User))
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), TokenScope{}, at.Scope))
		}
		c.Next()
	})
}

func (s *AccessToken) getToken(ctx context.Context, tokenStr string) (*models.AccessToken, error) {
	token, err := uuid.FromString(tokenStr)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid token (token: %s)", tokenStr)
	}

	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return models.GetUserByAccessTokenWithUser(ctx, db, token)
}

func (s *AccessToken) HasScope(scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query(common2.AccessTokenParamName) == "" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		at, err := s.getToken(c.Request.Context(), c.Query(common2.AccessTokenParamName))
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		if at == nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		for _, scope := range scopes {
			match := false
			for _, sc := range at.Scope {
				if sc == scope {
					match = true
					break
				}
			}
			if !match {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
		c.Next()
	}
}
