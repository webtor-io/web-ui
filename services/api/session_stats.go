package api

import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// SessionStatsData is one event of torrent-http-proxy's per-session stream
// (GET /session-stats/<infohash>): what this node delivered to one viewer's
// session for one torrent, over the last WindowSec seconds.
type SessionStatsData struct {
	WindowSec   float64 `json:"window_sec"`
	BytesPerSec float64 `json:"bytes_per_sec"`
	// Conns is the session's content requests for this torrent open right
	// now on the node: a point sample, taken once per event.
	Conns int `json:"conns"`
	// Active: a content request of the session for this torrent was open at
	// some moment since this stream's previous event -- one that opened and
	// closed between two events included, which Conns never sees (an HLS
	// segment fetched in a fraction of a second). A pointer because absent
	// and false are different answers: nil -- a thp from before the field,
	// which the reader judges by the old signals (statusview.Sample.Active).
	Active *bool `json:"active,omitempty"`
	// Rate is the tier's rate claim ("5M" = 5 Mbit/s), "" when there is none.
	Rate string `json:"rate,omitempty"`
	// Throttled is the share of the window those requests spent waiting in
	// the tier's bandwidth limiter, 0..1. A pointer because absent and zero
	// are different answers: nil — no limiter applied to any request (a plan
	// without a rate); 0 — a limiter that never had to wait.
	Throttled *float64 `json:"throttled,omitempty"`
}

// sessionStatsMaxLine caps one SSE line. An event is ~120 bytes; the cap
// only exists so a misbehaving upstream cannot grow a buffer per viewer.
const sessionStatsMaxLine = 16 * 1024

// SessionStats opens thp's per-session stream. u is a /session-stats URL on
// the host of an export URL rest-api returned for the torrent (the node that
// serves this viewer's bytes and holds the counters), with a token web-ui
// minted for it (handlers/resource sessionStatsToken); it goes through the
// same request path as Stats. No error it returns quotes u. Any non-200 is an error carrying the status
// (*StatusError): a thp without the route and a token without a session both
// answer that way, and the caller treats all of them as "unavailable". The
// channel closes on EOF, on a read error and when ctx is done.
func (s *Api) SessionStats(ctx context.Context, u string) (<-chan SessionStatsData, error) {
	req, err := s.makeTorrentHTTPProxyRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new request")
	}
	res, err := s.cl.Do(req)
	if err != nil {
		// A transport error quotes the URL, and the URL carries a token
		// minted for this stream: keep it out of every log line.
		var ue *url.Error
		if stderrors.As(err, &ue) {
			err = ue.Err
		}
		return nil, errors.Wrap(err, "failed to do request")
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, &StatusError{Status: res.StatusCode, RetryAfter: res.Header.Get("Retry-After")}
	}
	ch := make(chan SessionStatsData)
	go func() {
		b := res.Body
		defer func() {
			close(ch)
			_ = b.Close()
		}()
		scanner := bufio.NewScanner(b)
		// Same shape as Stats: a small initial buffer, never pre-allocated
		// to the cap — one of these lives per open status stream.
		scanner.Buffer(make([]byte, 0, 512), sessionStatsMaxLine)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var ev SessionStatsData
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
				// One a second per viewer: debug, or a bad upstream floods the log.
				log.WithError(err).WithField("line", line).Debug("session stats: skipping malformed event")
				continue
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
