package url_alias

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	common2 "github.com/webtor-io/web-ui/services/common"
)

type UrlAlias struct {
	domain string
	pg     *cs.PG
	urls   lazymap.LazyMap[string]
	codes  lazymap.LazyMap[*models.URLAlias]
	r      *gin.Engine
}

func New(pg *cs.PG, r *gin.Engine) *UrlAlias {
	return &UrlAlias{
		pg: pg,
		r:  r,
		urls: lazymap.New[string](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		codes: lazymap.New[*models.URLAlias](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}
func (s *UrlAlias) get(ctx context.Context, url string, proxy bool) (string, error) {
	db := s.pg.Get()
	if db == nil {
		return "", errors.New("db not initialized")
	}
	au, err := models.CreateOrGetURLAlias(ctx, db, url, proxy)
	if err != nil {
		return "", err
	}
	return au.Code, nil
}
func (s *UrlAlias) Get(ctx context.Context, url string, proxy bool) (string, error) {
	code, err := s.urls.Get(url, func() (string, error) {
		return s.get(ctx, url, proxy)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/s/%v", code), nil
}

func (s *UrlAlias) resolve(ctx context.Context, url string) (*models.URLAlias, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	au, err := models.GetURLAliasByCode(ctx, db, url)
	if err != nil {
		return nil, err
	}
	return au, nil
}

func (s *UrlAlias) Resolve(ctx context.Context, url string) (*models.URLAlias, error) {
	target, err := s.codes.Get(url, func() (*models.URLAlias, error) {
		return s.resolve(ctx, url)
	})
	if err != nil {
		return nil, err
	}
	return target, nil
}

func (s *UrlAlias) RegisterHandler(r *gin.Engine) {
	gr := r.Group("/s")
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: common.AnyMethods,
	}))
	gr.Match(common.AnyMethods, "/:code", s.handle)
	gr.Match(common.AnyMethods, "/:code/*rest", s.handle)
}

func (s *UrlAlias) handle(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Status(http.StatusBadRequest)
	}

	u, err := s.resolve(c.Request.Context(), code)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if u == nil {
		c.Status(http.StatusNotFound)
		return
	}
	pu, err := url.Parse(u.URL)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	pu.Path = strings.TrimSuffix(pu.Path, "/") + c.Param("rest")
	if u.Proxy {
		query := c.Request.URL.Query()
		c.Request.URL.RawQuery = query.Encode()
		c.Request.URL.Path = pu.Path
		c.Request.URL.RawPath = common2.EscapePath(pu.Path)
		c.Abort()
		s.r.HandleContext(c)
	} else {
		c.Redirect(http.StatusMovedPermanently, pu.String())
	}
}
