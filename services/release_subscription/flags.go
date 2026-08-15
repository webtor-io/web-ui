package release_subscription

import (
	"time"

	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/common"
)

const (
	PollBatchFlag       = "subscription-poll-batch"
	PollConcurrencyFlag = "subscription-poll-concurrency"
	PollMaxEpisodesFlag = "subscription-poll-max-episodes"
	NotifyIntervalFlag  = "subscription-notify-interval"
	IntervalHotFlag     = "subscription-interval-hot"
	IntervalPaidFlag    = "subscription-interval-paid"
	IntervalFreeFlag    = "subscription-interval-free"
	IntervalMaxFlag     = "subscription-interval-max"
	HotWindowFlag       = "subscription-hot-window"
)

// RegisterPollFlags declares the poller's scheduling policy.
//
// The defaults spend an account's budget where releases actually appear.
// A season of a currently-airing show is looked at every three hours for
// paying accounts, six otherwise, twelve on the free plan — and every one of
// those backs off on its own once the show goes quiet between seasons.
func RegisterPollFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   PollBatchFlag,
			Usage:  "max subscriptions polled in one run",
			Value:  300,
			EnvVar: "SUBSCRIPTION_POLL_BATCH",
		},
		cli.IntFlag{
			Name:   PollConcurrencyFlag,
			Usage:  "how many accounts are polled in parallel (one account's subscriptions always run in sequence — they share its indexers)",
			Value:  4,
			EnvVar: "SUBSCRIPTION_POLL_CONCURRENCY",
		},
		cli.IntFlag{
			Name:   PollMaxEpisodesFlag,
			Usage:  "max episodes of a season queried per poll, newest aired first",
			Value:  3,
			EnvVar: "SUBSCRIPTION_POLL_MAX_EPISODES",
		},
		cli.DurationFlag{
			Name:   NotifyIntervalFlag,
			Usage:  "shortest gap between two update emails for one subscription; everything found in between goes out together",
			Value:  12 * time.Hour,
			EnvVar: "SUBSCRIPTION_NOTIFY_INTERVAL",
		},
		cli.DurationFlag{
			Name:   IntervalHotFlag,
			Usage:  "poll interval while an episode is airing (paid accounts)",
			Value:  3 * time.Hour,
			EnvVar: "SUBSCRIPTION_INTERVAL_HOT",
		},
		cli.DurationFlag{
			Name:   IntervalPaidFlag,
			Usage:  "resting poll interval for paid accounts",
			Value:  6 * time.Hour,
			EnvVar: "SUBSCRIPTION_INTERVAL_PAID",
		},
		cli.DurationFlag{
			Name:   IntervalFreeFlag,
			Usage:  "poll interval for free accounts",
			Value:  12 * time.Hour,
			EnvVar: "SUBSCRIPTION_INTERVAL_FREE",
		},
		cli.DurationFlag{
			Name:   IntervalMaxFlag,
			Usage:  "ceiling for the idle backoff",
			Value:  24 * time.Hour,
			EnvVar: "SUBSCRIPTION_INTERVAL_MAX",
		},
		cli.DurationFlag{
			Name:   HotWindowFlag,
			Usage:  "how close to an episode's air date counts as airing",
			Value:  72 * time.Hour,
			EnvVar: "SUBSCRIPTION_HOT_WINDOW",
		},
	)
}

// NewPollConfig reads the policy off the CLI context. Domain and session
// secret come from the shared flags: the first builds the links in an email,
// the second signs its unsubscribe token.
func NewPollConfig(c *cli.Context) PollConfig {
	return PollConfig{
		Batch:          c.Int(PollBatchFlag),
		Concurrency:    c.Int(PollConcurrencyFlag),
		MaxEpisodes:    c.Int(PollMaxEpisodesFlag),
		NotifyInterval: c.Duration(NotifyIntervalFlag),
		IntervalHot:    c.Duration(IntervalHotFlag),
		IntervalPaid:   c.Duration(IntervalPaidFlag),
		IntervalFree:   c.Duration(IntervalFreeFlag),
		IntervalMax:    c.Duration(IntervalMaxFlag),
		HotWindow:      c.Duration(HotWindowFlag),
		Domain:         c.String(common.DomainFlag),
		Secret:         c.String(common.SessionSecretFlag),
	}
}
