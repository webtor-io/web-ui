package j

import (
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/thumbnail"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	warmupTimeoutMinFlag            = "warmup-timeout-min"
	warmupNoPeersTimeoutSecFlag     = "warmup-no-peers-timeout-sec"
	warmupSlowPeersTimeoutSecFlag   = "warmup-slow-peers-timeout-sec"
	warmupSeederProbeTimeoutSecFlag = "warmup-seeder-probe-timeout-sec"
	graceRulesEnabledFlag           = "grace-rules-enabled"
	graceDurationSecFlag            = "grace-duration-sec"
	graceRateFlag                   = "grace-rate"
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
		cli.IntFlag{
			Name:   warmupSeederProbeTimeoutSecFlag,
			Usage:  "warmup seeder fast-path probe budget (sec): time to wait for the first stats SSE event; if all head/tail pieces are already Complete on the pod, the full torrent warmup is skipped. 0 disables the probe.",
			EnvVar: "WARMUP_SEEDER_PROBE_TIMEOUT_SEC",
			Value:  3,
		},
		cli.BoolFlag{
			Name:   graceRulesEnabledFlag,
			Usage:  "enable grace rules",
			EnvVar: "GRACE_RULES_ENABLED",
		},
		cli.IntFlag{
			Name:   graceDurationSecFlag,
			Usage:  "grace duration",
			EnvVar: "GRACE_DURATION_SEC",
			Value:  1200,
		},
		cli.StringFlag{
			Name:   graceRateFlag,
			Usage:  "grace rate",
			EnvVar: "GRACE_RATE",
			Value:  "50M",
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
	thumbnail     *thumbnail.Service
	warmup        scripts.WarmupSettings
	grace         scripts.GraceSettings
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

func New(c *cli.Context, q *job.Queues, tm *template.Manager[*web.Context], api *api.Api, enricher *enrich.Enricher, i18nSvc *i18n.Service, userSubtitles *us.Service, thumb *thumbnail.Service) *Jobs {
	return &Jobs{
		q:             q,
		tb:            tm,
		api:           api,
		enricher:      enricher,
		i18n:          i18nSvc,
		userSubtitles: userSubtitles,
		thumbnail:     thumb,
		warmup: scripts.WarmupSettings{
			TimeoutMin:            c.Int(warmupTimeoutMinFlag),
			NoPeersTimeoutSec:     c.Int(warmupNoPeersTimeoutSecFlag),
			SlowPeersTimeoutSec:   c.Int(warmupSlowPeersTimeoutSecFlag),
			SeederProbeTimeoutSec: c.Int(warmupSeederProbeTimeoutSecFlag),
		},
		grace: scripts.GraceSettings{
			Enabled:     c.Bool(graceRulesEnabledFlag),
			DurationSec: c.Int(graceDurationSecFlag),
			Rate:        c.String(graceRateFlag),
		},
	}
}
