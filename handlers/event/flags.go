package event

import "github.com/urfave/cli"

const (
	useEventHandlerFlag       = "use-event-handler"
	winbackHoldoutFlag        = "winback-holdout"
	winbackTrialEndedFromFlag = "winback-trial-ended-from"
	defaultWinbackHoldout     = 0
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolTFlag{
			Name:   useEventHandlerFlag,
			Usage:  "use event handler",
			EnvVar: "USE_EVENT_HANDLER",
		},
		cli.IntFlag{
			Name:   winbackHoldoutFlag,
			Usage:  "percent of accounts eligible for the winback letter that do not get it, as a control group (0 = everyone gets it)",
			Value:  defaultWinbackHoldout,
			EnvVar: "WINBACK_HOLDOUT",
		},
		cli.StringFlag{
			Name:   winbackTrialEndedFromFlag,
			Usage:  "RFC 3339 instant from which a cancelled free trial earns the winback letter; empty keeps that reason off (a declined card that the provider gave up on earns it regardless)",
			EnvVar: "WINBACK_TRIAL_ENDED_FROM",
		},
	)
}
