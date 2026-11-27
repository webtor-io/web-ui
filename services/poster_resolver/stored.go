package poster_resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// SourceDiscover is the poster the user saw in Discover when they started the
// torrent (models.ResourcePoster).
const SourceDiscover SourceKind = "discover"

const maxStoredPosterURL = 2048

// ValidateStoredPosterURL accepts only public http(s) poster URLs. The URL
// comes from the browser and is later fetched by the server, so a host that
// resolves to a loopback, private or link-local address (the other services in
// this container, cloud metadata endpoints) is refused.
func ValidateStoredPosterURL(ctx context.Context, raw string) error {
	if raw == "" || len(raw) > maxStoredPosterURL {
		return errors.New("poster url: empty or too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.Wrap(err, "poster url: invalid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("poster url: scheme must be http or https")
	}
	if u.User != nil || u.Hostname() == "" {
		return errors.New("poster url: no credentials, host required")
	}
	host := u.Hostname()
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addrs) == 0 {
			return errors.Errorf("poster url: cannot resolve %s", host)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	for _, ip := range ips {
		if isInternalIP(ip) {
			return errors.Errorf("poster url: %s resolves to an internal address", host)
		}
	}
	return nil
}

func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() ||
		// 100.64.0.0/10 carrier-grade NAT
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) ||
		strings.HasPrefix(ip.String(), "169.254.")
}

// storedPosterSource serves the poster stored for a resource. The URL is
// checked again at fetch time, not only when it was stored.
func storedPosterSource(posterURL string, cl *http.Client) *Source {
	sum := sha256.Sum256([]byte(posterURL))
	inner := posterSource(SourceDiscover, hex.EncodeToString(sum[:8]), posterURL, cl)
	fetch := inner.Fetch
	inner.Fetch = func(ctx context.Context) (image.Image, error) {
		if err := ValidateStoredPosterURL(ctx, posterURL); err != nil {
			return nil, errors.Wrapf(ErrNotFound, "%v", err)
		}
		return fetch(ctx)
	}
	return inner
}
