package url_alias

import (
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	"net/http"
	"net/url"
	"time"
)

type UrlAlias struct {
	domain string
	pg     *cs.PG
	urls   lazymap.LazyMap[string]
	codes  lazymap.LazyMap[string]
}

func New(pg *cs.PG) *UrlAlias {
	return &UrlAlias{
		pg: pg,
		urls: lazymap.New[string](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		codes: lazymap.New[string](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}
func (s *UrlAlias) get(url string) (string, error) {
	db := s.pg.Get()
	if db == nil {
		return "", errors.New("db not initialized")
	}
	au, err := models.CreateOrGetURLAlias(db, url)
	if err != nil {
		return "", err
	}
	return au.Code, nil
}
func (s *UrlAlias) Get(url string) (string, error) {
	code, err := s.urls.Get(url, func() (string, error) {
		return s.get(url)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/s/%v", code), nil
}

func (s *UrlAlias) resolve(url string) (string, error) {
	db := s.pg.Get()
	if db == nil {
		return "", errors.New("db not initialized")
	}
	au, err := models.GetURLAliasByCode(db, url)
	if err != nil {
		return "", err
	}
	return au.URL, nil
}

func (s *UrlAlias) Resolve(url string) (string, error) {
	target, err := s.codes.Get(url, func() (string, error) {
		return s.resolve(url)
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

func (s *UrlAlias) RegisterHandler(r *gin.Engine) {
	gr := r.Group("/s")
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
	}))
	gr.GET("/:code", s.handle)
	gr.GET("/:code/*rest", s.handle)
}

func (s *UrlAlias) handle(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Status(http.StatusBadRequest)
	}

	u, err := s.resolve(code)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if u == "" {
		c.Status(http.StatusNotFound)
		return
	}
	pu, err := url.Parse(u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	pu.Path = pu.Path + c.Param("rest")
	c.Redirect(http.StatusMovedPermanently, pu.String())
}
