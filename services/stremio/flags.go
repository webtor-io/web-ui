package stremio

import "github.com/urfave/cli"

const (
	StremioUserAgentFlag = "stremio-addon-user-agent"
	StremioProxyFlag     = "stremio-addon-proxy"

	// StremioAllowPrivateNetworkFlag lifts the private-address ban on the
	// addon fetch. Same shape and same reasoning as the Torznab flag: a
	// self-hoster may legitimately run an addon on their LAN, but on a
	// shared deployment a user-supplied addon URL pointing inward is SSRF.
	StremioAllowPrivateNetworkFlag = "stremio-addon-allow-private-network"
)

func RegisterClientFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   StremioUserAgentFlag,
			Usage:  "user agent for stremio addon http client",
			EnvVar: "STREMIO_ADDON_USER_AGENT",
		},
		cli.StringFlag{
			Name:   StremioProxyFlag,
			Usage:  "proxy URL for stremio addon http client (e.g., http://proxy:8080 or socks5://proxy:1080)",
			EnvVar: "STREMIO_ADDON_PROXY",
		},
		cli.BoolFlag{
			Name:   StremioAllowPrivateNetworkFlag,
			Usage:  "allow stremio addon URLs that resolve to private or loopback addresses (self-hosted only)",
			EnvVar: "STREMIO_ADDON_ALLOW_PRIVATE_NETWORK",
		},
	)
}
