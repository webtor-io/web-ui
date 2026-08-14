package torznab

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
)

// maxRedirects bounds the manual redirect walk in ResolveInfoHash. The
// download client does not follow redirects itself — see newHTTPClient.
const maxRedirects = 5

// ResolveInfoHash produces the v1 infohash for a search result.
//
// Feeds vary a lot in how much they give away: Jackett usually ships an
// infohash attribute for public trackers, Prowlarr often ships only a
// magneturl, and private trackers ship neither — just a download link that
// serves the .torrent (and needs the API key that is already baked into the
// link). The cheap sources are tried first; the download is the last resort
// because it costs a request per result.
func (c *Client) ResolveInfoHash(ctx context.Context, r *Result) (string, error) {
	if h := normalizeInfoHash(r.InfoHash); h != "" {
		return h, nil
	}
	if h := infoHashFromMagnet(r.MagnetURI); h != "" {
		return h, nil
	}
	if h := infoHashFromMagnet(r.Link); h != "" {
		return h, nil
	}
	if r.Link == "" {
		return "", errors.New("result carries neither infohash nor download link")
	}
	return c.infoHashFromDownload(ctx, r.Link)
}

// NeedsDownload reports whether resolving this result's infohash requires
// fetching its download link, as opposed to reading a field the feed already
// provided. Callers use it to budget those fetches.
func NeedsDownload(r *Result) bool {
	return normalizeInfoHash(r.InfoHash) == "" &&
		infoHashFromMagnet(r.MagnetURI) == "" &&
		infoHashFromMagnet(r.Link) == ""
}

// infoHashFromDownload walks the download link. Public-tracker links
// typically redirect to a magnet URI, which is the answer without a body;
// everything else is read as a .torrent.
func (c *Client) infoHashFromDownload(ctx context.Context, link string) (string, error) {
	current := link
	for i := 0; i < maxRedirects; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", errors.Wrap(err, "failed to create download request")
		}
		req.Header.Set("Accept", "application/x-bittorrent, */*")
		if c.ua != "" {
			req.Header.Set("User-Agent", c.ua)
		}
		resp, err := c.dl.Do(req)
		if err != nil {
			// Download links carry the key too (Jackett's /dl/ uses
			// jackett_apikey), and this error is logged per result.
			return "", errors.Wrap(RedactError(err), "failed to fetch torrent file")
		}

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			_ = resp.Body.Close()
			if loc == "" {
				return "", errors.Errorf("download link returned HTTP %d without a location", resp.StatusCode)
			}
			if h := infoHashFromMagnet(loc); h != "" {
				return h, nil
			}
			next, err := resolveReference(current, loc)
			if err != nil {
				return "", err
			}
			// Anything that is neither http(s) nor a magnet is a dead end:
			// the transport cannot dial it, and following it would only
			// turn a clear message into "unsupported protocol scheme".
			if !isHTTPURL(next) {
				return "", errors.Errorf("download link redirected to an unusable location %q", loc)
			}
			current = next
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return "", errors.Errorf("download link returned HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if err != nil {
			return "", errors.Wrap(err, "failed to read torrent file")
		}
		return infoHashFromTorrent(body)
	}
	return "", errors.New("too many redirects on download link")
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func resolveReference(base, loc string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", errors.Wrap(err, "invalid download link")
	}
	l, err := url.Parse(loc)
	if err != nil {
		return "", errors.Wrap(err, "invalid redirect location")
	}
	return b.ResolveReference(l).String(), nil
}

// infoHashFromTorrent parses a .torrent body. metainfo.Load only bencode-
// decodes the outer dictionary, so a truncated or HTML body fails here
// rather than deeper in the pipeline.
func infoHashFromTorrent(body []byte) (string, error) {
	mi, err := metainfo.Load(bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "download link did not return a torrent file")
	}
	h := mi.HashInfoBytes()
	if h.IsZero() {
		return "", errors.New("torrent file has an empty infohash")
	}
	return strings.ToLower(h.HexString()), nil
}

// infoHashFromMagnet pulls the btih out of a magnet URI, accepting both the
// 40-char hex and 32-char base32 spellings.
func infoHashFromMagnet(magnet string) string {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(magnet)), "magnet:") {
		return ""
	}
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		const prefix = "urn:btih:"
		if !strings.HasPrefix(strings.ToLower(xt), prefix) {
			continue
		}
		if h := normalizeInfoHash(xt[len(prefix):]); h != "" {
			return h
		}
	}
	return ""
}

// normalizeInfoHash returns a lowercase 40-char hex infohash, converting from
// base32 when needed, or "" when the input is not a v1 infohash.
func normalizeInfoHash(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	switch len(s) {
	case 40:
		if _, err := hex.DecodeString(s); err != nil {
			return ""
		}
		return strings.ToLower(s)
	case 32:
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(s))
		if err != nil || len(b) != 20 {
			return ""
		}
		return hex.EncodeToString(b)
	}
	return ""
}
