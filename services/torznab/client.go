package torznab

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	jackett "github.com/webtor-io/go-jackett"
)

// maxBodyBytes caps what we read from an indexer on the download path. The
// search path is handled by go-jackett.
const maxBodyBytes = 8 << 20

// maxPlausibleSeeders is the ceiling above which a swarm count is treated as
// garbage rather than as a very popular torrent. The largest public swarms
// are in the tens of thousands.
const maxPlausibleSeeders = 1 << 20

// Client talks Torznab to arbitrary user-supplied endpoints.
//
// The protocol itself lives in github.com/webtor-io/go-jackett (a Torznab
// client despite the name); this type owns everything around it that is
// specific to running the protocol against URLs a user typed in: the egress
// policy, the result cap, and turning a result into something addressable
// by infohash.
//
// It is safe for concurrent use and holds no per-user state — the endpoint
// travels with each call.
type Client struct {
	cl         *http.Client
	dl         *http.Client
	ua         string
	timeout    time.Duration
	maxResults int
}

// Options is the client configuration, separated from the cli.Context so
// tests can build a client without one.
type Options struct {
	Timeout             time.Duration
	MaxResults          int
	UserAgent           string
	AllowPrivateNetwork bool
}

func New(c *cli.Context) *Client {
	return NewWithOptions(Options{
		Timeout:             c.Duration(TimeoutFlag),
		MaxResults:          c.Int(MaxResultsFlag),
		UserAgent:           c.String(UserAgentFlag),
		AllowPrivateNetwork: c.Bool(AllowPrivateNetworkFlag),
	})
}

func NewWithOptions(o Options) *Client {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxResults <= 0 {
		o.MaxResults = 30
	}
	return &Client{
		cl:         newHTTPClient(o.AllowPrivateNetwork, o.Timeout, false),
		dl:         newHTTPClient(o.AllowPrivateNetwork, o.Timeout, true),
		ua:         o.UserAgent,
		timeout:    o.Timeout,
		maxResults: o.MaxResults,
	}
}

// Timeout is the per-search budget, exposed so the stream layer can size its
// own context to match.
func (c *Client) Timeout() time.Duration {
	return c.timeout
}

// MaxResults is how many results survive a single query, highest seeded
// first.
func (c *Client) MaxResults() int {
	return c.maxResults
}

// HTTP exposes the search client so sibling services (the Cinemeta title
// lookup) inherit the same timeouts and egress policy.
func (c *Client) HTTP() *http.Client {
	return c.cl
}

// newHTTPClient builds a client whose dialer refuses private/loopback
// destinations unless the deployment opted in. The check sits in the dialer
// rather than on the parsed URL because that is the only place that sees the
// address actually connected to — a hostname resolving to 10.0.0.1, or a
// redirect into the cluster, both land here.
//
// forDownload clients stop at the first redirect instead of following it:
// download links commonly 302 to a magnet: URI, which the transport cannot
// dial, and we want to read that Location rather than fail on it.
func newHTTPClient(allowPrivate bool, timeout time.Duration, forDownload bool) *http.Client {
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		d.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return errors.Errorf("cannot resolve %s", address)
			}
			if isPrivateIP(ip) {
				return errors.Errorf("refusing to connect to private address %s", ip)
			}
			return nil
		}
	}
	cl := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			MaxIdleConnsPerHost:   4,
		},
	}
	if forDownload {
		cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return cl
}

// blockedRanges are the ranges Go's own predicates miss: carrier-grade NAT
// (which is also where Alibaba's metadata service lives), benchmarking,
// IETF protocol assignments, reserved space, and the two IPv6 transition
// mechanisms that embed an arbitrary IPv4 address — 6to4 and NAT64 — through
// which 2002:7f00:1:: reaches 127.0.0.1.
var blockedRanges = []*net.IPNet{
	mustCIDR("100.64.0.0/10"),
	mustCIDR("192.0.0.0/24"),
	mustCIDR("198.18.0.0/15"),
	mustCIDR("240.0.0.0/4"),
	mustCIDR("2002::/16"),
	mustCIDR("64:ff9b::/96"),
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// isPrivateIP reports whether an address must not be reachable from a shared
// deployment. The base rule is "global unicast only", which excludes
// loopback, link-local (and with it the 169.254.169.254 metadata endpoint),
// multicast and the unspecified address in one go; IsPrivate covers
// RFC1918/RFC4193, and blockedRanges the rest.
func isPrivateIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// jackettClient builds a per-endpoint go-jackett client. It is cheap
// (no connections are made) and deliberately per-call: go-jackett attaches
// the API key to the client, so one client per user endpoint is what keeps
// two users of the same host from borrowing each other's key.
func (c *Client) jackettClient(ep Endpoint) (*jackett.Client, error) {
	if err := validateEndpointURL(ep.URL); err != nil {
		return nil, err
	}
	j, err := jackett.NewTorznab(jackett.Settings{
		ApiURL:    strings.TrimSpace(ep.URL),
		ApiKey:    ep.APIKey,
		Client:    c.cl,
		UserAgent: c.ua,
	})
	if err != nil {
		return nil, errors.Wrap(err, "invalid indexer URL")
	}
	return j, nil
}

func validateEndpointURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.Wrap(err, "invalid indexer URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("indexer URL must use http or https")
	}
	if u.Host == "" {
		return errors.New("indexer URL has no host")
	}
	return nil
}

// Caps probes an endpoint's capabilities. Beyond filling the snapshot we
// persist, a successful caps call is what "the indexer works" means at add
// time — it needs no query and every Torznab implementation answers it.
func (c *Client) Caps(ctx context.Context, ep Endpoint) (*jackett.IndexerCaps, error) {
	j, err := c.jackettClient(ep)
	if err != nil {
		return nil, err
	}
	caps, err := j.Caps(ctx, "")
	if err != nil {
		return nil, errors.Wrap(RedactError(err), "indexer did not answer a capabilities request")
	}
	return &caps, nil
}

// Search runs one query and returns results sorted by seeders, capped at
// MaxResults.
//
// Capability validation is skipped: TorznabStream already picks the query
// shape from the caps snapshot it persisted at add time, and letting
// go-jackett re-check would add a caps round-trip to every search.
func (c *Client) Search(ctx context.Context, ep Endpoint, q Query) ([]Result, error) {
	j, err := c.jackettClient(ep)
	if err != nil {
		return nil, err
	}
	fr, err := q.fetchRequest()
	if err != nil {
		return nil, err
	}
	results, err := j.Fetch(ctx, fr, jackett.WithoutCapsValidation())
	if err != nil {
		return nil, errors.Wrap(RedactError(err), "indexer search failed")
	}
	for i := range results {
		if results[i].Tracker == "" {
			results[i].Tracker = trackerFromURLs(results[i].ID, results[i].Link)
		}
		// Feeds that report "unknown" as seeders="-1" parse into a huge
		// uint. Left alone those items sort above everything real and, at
		// 30 of them, evict every genuine result before the cap.
		if results[i].Seeders > maxPlausibleSeeders {
			results[i].Seeders = 0
		}
		if results[i].Peers > maxPlausibleSeeders {
			results[i].Peers = 0
		}
	}
	sort.SliceStable(results, func(i, k int) bool {
		return results[i].Seeders > results[k].Seeders
	})
	if len(results) > c.maxResults {
		results = results[:c.maxResults]
	}
	return results, nil
}

// trackerFromURLs extracts the indexer id out of a Jackett URL
// (…/indexers/<id>/results/torznab/…), or falls back to the host. Only used
// for feeds that don't name themselves — Jackett tags every item with
// <jackettindexer>, Prowlarr and bare feeds do not.
func trackerFromURLs(candidates ...string) string {
	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		if _, after, ok := strings.Cut(raw, "/indexers/"); ok {
			id, _, _ := strings.Cut(after, "/")
			if id != "" && id != "all" {
				return id
			}
		}
	}
	for _, raw := range candidates {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			return u.Hostname()
		}
	}
	return ""
}

// IsUnreachable reports whether an error means "our servers cannot reach
// this URL", as opposed to the indexer answering and rejecting us.
//
// The distinction is the whole difference between the two mistakes users
// make: a wrong API key is fixed on the form, an address that only exists
// on their LAN cannot be fixed there at all. Both otherwise surface as the
// same generic failure.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"refusing to connect to private address",
		"connection refused",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"tls handshake",
		"certificate",
		"context deadline exceeded",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
