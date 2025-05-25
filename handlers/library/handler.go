package library

import (
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/handlers/library/helpers"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"
)

const (
	awsPosterCacheBucket = "aws-poster-cache-bucket"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   awsPosterCacheBucket,
			Usage:  "aws poster cache bucket",
			EnvVar: "AWS_POSTER_CACHE_BUCKET",
		},
	)
}

type Handler struct {
	tb                  template.Builder[*web.Context]
	api                 *api.Api
	pg                  *cs.PG
	jobs                *job.Handler
	cl                  *http.Client
	s3Cl                *cs.S3Client
	posterCacheS3Bucket string
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, pg *cs.PG, jobs *job.Handler, cl *http.Client, s3Cl *cs.S3Client) {
	h := &Handler{
		tb: tm.MustRegisterViews("library/*").
			WithHelper(helpers.NewStarsHelper()).
			WithHelper(helpers.NewMenuHelper()).
			WithHelper(helpers.NewSortHelper()).
			WithHelper(helpers.NewVideoContentHelper()).
			WithLayout("main"),
		api:                 api,
		pg:                  pg,
		jobs:                jobs,
		cl:                  cl,
		s3Cl:                s3Cl,
		posterCacheS3Bucket: c.String(awsPosterCacheBucket),
	}
	r.GET("/lib", h.index)
	r.GET("/lib/:type", h.index)
	r.GET("/lib/:type/poster/:imdb_id/:file", h.poster)
	r.POST("/lib/add", h.add)
	r.POST("/lib/remove", h.remove)
}
