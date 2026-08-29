package stremio

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/egress"
)

// NewClient builds the client that fetches a user-supplied Stremio addon URL.
//
// The URL comes from the user, so the destination is theirs to choose. The
// handler's own checks (scheme, a manifest.json suffix, a three-name denylist)
// are not an egress policy: they compare the string typed in, so 127.0.0.2,
// [::1], the link-local metadata address and any cluster DNS name all pass —
// and following a redirect discards even the path constraint.
//
// So the rule lives where it can see the address actually dialled, and
// redirects are not followed. This mirrors services/torznab, which has done
// it this way all along; the shared predicate is in services/egress.
func NewClient(c *cli.Context) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   egress.DialControl(c.Bool(StremioAllowPrivateNetworkFlag)),
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	// Configure proxy if provided. With a proxy the dialer only ever sees
	// the proxy's address, so the egress decision moves to the proxy — the
	// same trade-off Torznab makes.
	proxyURL := c.String(StremioProxyFlag)
	if proxyURL != "" {
		if parsedURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsedURL)
		}
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return client
}
