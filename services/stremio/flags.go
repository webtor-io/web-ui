package stremio

import "github.com/urfave/cli"

const (
	StremioUserAgentFlag = "stremio-addon-user-agent"
	StremioProxyFlag     = "stremio-addon-proxy"
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
	)
}
