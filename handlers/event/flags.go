package event

import "github.com/urfave/cli"

const (
	useEventHandlerFlag = "use-event-handler"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolTFlag{
			Name:   useEventHandlerFlag,
			Usage:  "use event handler",
			EnvVar: "USE_EVENT_HANDLER",
		},
	)
}
