package stremio

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/urfave/cli"
)

func NewClient(c *cli.Context) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
	}

	// Configure proxy if provided
	proxyURL := c.String(StremioProxyFlag)
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
