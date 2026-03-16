package j

import (
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	warmupTimeoutMinFlag = "warmup-timeout-min"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   warmupTimeoutMinFlag,
			Usage:  "warmup timeout min",
			EnvVar: "WARMUP_TIMEOUT_MIN",
			Value:  10,
		},
	)
}

type Jobs struct {
	q                *job.Queues
	tb               template.Builder[*web.Context]
	api              *api.Api
	enricher         *enrich.Enricher
	warmupTimeoutMin int
}

func New(c *cli.Context, q *job.Queues, tm *template.Manager[*web.Context], api *api.Api, enricher *enrich.Enricher) *Jobs {
	return &Jobs{
		q:                q,
		tb:               tm,
		api:              api,
		enricher:         enricher,
		warmupTimeoutMin: c.Int(warmupTimeoutMinFlag),
	}
}
