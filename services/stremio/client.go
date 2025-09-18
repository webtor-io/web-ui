package stremio

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/urfave/cli"
)

const (
	stremioUserAgentFlag = "stremio-addon-user-agent"
	stremioProxyFlag     = "stremio-addon-proxy"
)

func RegisterClientFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   stremioUserAgentFlag,
			Usage:  "user agent for stremio addon http client",
			EnvVar: "STREMIO_ADDON_USER_AGENT",
		},
		cli.StringFlag{
			Name:   stremioProxyFlag,
			Usage:  "proxy URL for stremio addon http client (e.g., http://proxy:8080 or socks5://proxy:1080)",
			EnvVar: "STREMIO_ADDON_PROXY",
		},
	)
}

func NewClient(c *cli.Context) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
	}

	// Configure proxy if provided
	proxyURL := c.String(stremioProxyFlag)
	if proxyURL != "" {
		if parsedURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsedURL)
		}
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	return client
}

func GetUserAgent(c *cli.Context) string {
	return c.String(stremioUserAgentFlag)
}
