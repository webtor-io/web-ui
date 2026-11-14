package private_torrent

import (
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/metrics"
)

// ponytail: one process-wide switch; it goes in phase 4, the enforce behaviour stays
const EnforceFlag = "private-access-enforce"

var enforce atomic.Bool

// RegisterFlags adds --private-access-enforce (PRIVATE_ACCESS_ENFORCE), off
// by default. On, it opens Vault pledges on private torrents.
func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.BoolFlag{
		Name:   EnforceFlag,
		Usage:  "private torrents are enforced: Vault pledges on them are open",
		EnvVar: "PRIVATE_ACCESS_ENFORCE",
	})
}

// Configure sets the switch from the flag and says at start which way it
// stands.
func Configure(c *cli.Context) {
	on := c.Bool(EnforceFlag)
	SetEnforce(on)
	pledges := "closed"
	if on {
		pledges = "open"
	}
	log.Infof("private torrents: PRIVATE_ACCESS_ENFORCE=%t, private Vault pledges %s", on, pledges)
}

// Enforce reports whether the switch is on.
func Enforce() bool {
	return enforce.Load()
}

// SetEnforce turns the switch and its gauge (webui_private_enforce).
func SetEnforce(on bool) {
	enforce.Store(on)
	metrics.PrivateEnforce(on)
}
