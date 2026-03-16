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
	warmupTimeoutMinFlag       = "warmup-timeout-min"
	useSessionTranscoderFlag   = "use-session-transcoder"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   warmupTimeoutMinFlag,
			Usage:  "warmup timeout min",
			EnvVar: "WARMUP_TIMEOUT_MIN",
			Value:  10,
		},
		cli.BoolFlag{
			Name:   useSessionTranscoderFlag,
			Usage:  "use session-based transcoder (hls-staging)",
			EnvVar: "USE_SESSION_TRANSCODER",
		},
	)
}

type Jobs struct {
	q                    *job.Queues
	tb                   template.Builder[*web.Context]
	api                  *api.Api
	enricher             *enrich.Enricher
	warmupTimeoutMin     int
	useSessionTranscoder bool
}

func New(c *cli.Context, q *job.Queues, tm *template.Manager[*web.Context], api *api.Api, enricher *enrich.Enricher) *Jobs {
	return &Jobs{
		q:                    q,
		tb:                   tm,
		api:                  api,
		enricher:             enricher,
		warmupTimeoutMin:     c.Int(warmupTimeoutMinFlag),
		useSessionTranscoder: c.Bool(useSessionTranscoderFlag),
	}
}
