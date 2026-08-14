package torznab

import (
	"time"

	"github.com/urfave/cli"
)

const (
	TimeoutFlag             = "torznab-timeout"
	MaxResultsFlag          = "torznab-max-results"
	UserAgentFlag           = "torznab-user-agent"
	AllowPrivateNetworkFlag = "torznab-allow-private-network"
	ProxyFlag               = "torznab-proxy"
)

// DefaultTimeout is deliberately well above the 5s CompositeStream budget
// used for Stremio addons: a Jackett "all indexers" query fans out to every
// configured tracker and routinely needs ten seconds or more. CompositeStream
// honours it through the TimeoutedService interface.
const DefaultTimeout = 12 * time.Second

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.DurationFlag{
			Name:   TimeoutFlag,
			Usage:  "timeout for a single Torznab search",
			Value:  DefaultTimeout,
			EnvVar: "TORZNAB_TIMEOUT",
		},
		cli.IntFlag{
			Name:   MaxResultsFlag,
			Usage:  "max results kept per Torznab indexer per query (highest seeded first)",
			Value:  30,
			EnvVar: "TORZNAB_MAX_RESULTS",
		},
		cli.StringFlag{
			Name:   UserAgentFlag,
			Usage:  "user agent sent to Torznab indexers",
			Value:  "webtor.io",
			EnvVar: "TORZNAB_USER_AGENT",
		},
		cli.StringFlag{
			Name: ProxyFlag,
			Usage: "HTTP/SOCKS5 proxy for Torznab requests, e.g. socks5://user:pass@host:1080. " +
				"Consumer ISPs routinely drop inbound connections from datacenter ranges, so an " +
				"indexer that is reachable from a browser can still be unreachable from the cluster",
			EnvVar: "TORZNAB_PROXY",
		},
		cli.BoolFlag{
			Name: AllowPrivateNetworkFlag,
			Usage: "allow Torznab indexer URLs that resolve to private/loopback addresses " +
				"(self-hosted deployments only — on shared infrastructure this is an SSRF hole)",
			EnvVar: "TORZNAB_ALLOW_PRIVATE_NETWORK",
		},
	)
}
