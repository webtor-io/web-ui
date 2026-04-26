package j

import (
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	warmupTimeoutMinFlag          = "warmup-timeout-min"
	warmupNoPeersTimeoutSecFlag   = "warmup-no-peers-timeout-sec"
	warmupSlowPeersTimeoutSecFlag = "warmup-slow-peers-timeout-sec"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   warmupTimeoutMinFlag,
			Usage:  "warmup timeout min",
			EnvVar: "WARMUP_TIMEOUT_MIN",
			Value:  3,
		},
		cli.IntFlag{
			Name:   warmupNoPeersTimeoutSecFlag,
			Usage:  "warmup watchdog cutoff (sec): if no bytes and no peers within this window, surface the no_peers CTA early",
			EnvVar: "WARMUP_NO_PEERS_TIMEOUT_SEC",
			Value:  60,
		},
		cli.IntFlag{
			Name:   warmupSlowPeersTimeoutSecFlag,
			Usage:  "warmup watchdog cutoff (sec): if downloaded bytes are still below the early-min threshold within this window, surface the no_peers CTA early",
			EnvVar: "WARMUP_SLOW_PEERS_TIMEOUT_SEC",
			Value:  120,
		},
	)
}

type Jobs struct {
	q             *job.Queues
	tb            template.Builder[*web.Context]
	api           *api.Api
	enricher      *enrich.Enricher
	i18n          *i18n.Service
	userSubtitles *us.Service
	warmup        scripts.WarmupSettings
}

// T translates a message key using the language from web.Context.
func (s *Jobs) T(c *web.Context, key string) string {
	return i18n.TranslateWithLocalizer(s.i18n.Localizer(c.Lang), key)
}

// errorFormatter returns an ErrorFormatter that classifies errors and
// translates them to the user's language.
func (s *Jobs) errorFormatter(c *web.Context) job.ErrorFormatter {
	loc := s.i18n.Localizer(c.Lang)
	return func(err error) string {
		key := web.ClassifyError(err)
		return i18n.TranslateWithLocalizer(loc, key)
	}
}

func New(c *cli.Context, q *job.Queues, tm *template.Manager[*web.Context], api *api.Api, enricher *enrich.Enricher, i18nSvc *i18n.Service, userSubtitles *us.Service) *Jobs {
	return &Jobs{
		q:             q,
		tb:            tm,
		api:           api,
		enricher:      enricher,
		i18n:          i18nSvc,
		userSubtitles: userSubtitles,
		warmup: scripts.WarmupSettings{
			TimeoutMin:          c.Int(warmupTimeoutMinFlag),
			NoPeersTimeoutSec:   c.Int(warmupNoPeersTimeoutSecFlag),
			SlowPeersTimeoutSec: c.Int(warmupSlowPeersTimeoutSecFlag),
		},
	}
}
