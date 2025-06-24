package access_token

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services"
	"github.com/webtor-io/web-ui/services/auth"
	"net/http"
	"strings"
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
	return models.MakeAccessToken(db, u.ID, name, scope)
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
	return models.GetAccessTokenByName(db, u.ID, name)
}

type TokenScope struct{}

func (s *AccessToken) RegisterHandler(r *gin.Engine) {
	prefix := fmt.Sprintf("/%s/", services.AccessTokenParamName)
	r.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, prefix) {
			c.Next()
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 2)
		if len(parts) < 2 {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		token := parts[0]
		rest := "/" + parts[1]

		query := c.Request.URL.Query()
		query.Set(services.AccessTokenParamName, token)
		c.Request.URL.RawQuery = query.Encode()
		c.Request.URL.Path = rest
		c.Abort()
		r.HandleContext(c)
	})
	r.Use(func(c *gin.Context) {
		if c.Query(services.AccessTokenParamName) == "" {
			c.Next()
			return
		}
		at, err := s.getToken(c.Query(services.AccessTokenParamName))
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		if at != nil {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, at.User))
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), TokenScope{}, at.Scope))
		}
		c.Next()
	})
}

func (s *AccessToken) getToken(tokenStr string) (*models.AccessToken, error) {
	token, err := uuid.FromString(tokenStr)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid token (token: %s)", tokenStr)
	}

	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return models.GetUserByAccessTokenWithUser(db, token)
}

func (s *AccessToken) HasScope(scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query(services.AccessTokenParamName) == "" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		at, err := s.getToken(c.Query(services.AccessTokenParamName))
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
