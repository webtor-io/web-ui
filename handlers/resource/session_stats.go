package resource

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/statusview"
)

// The viewer's own link on the status chain ("cache ▸ you") and the plan's
// cap come from torrent-http-proxy's per-session stream
// (GET /session-stats/<infohash>): the node that serves this viewer's bytes
// counts what it delivered to their session and how long those requests
// waited in the tier's bandwidth limiter. services/statusview turns the
// events into what the chain says; this file only finds the stream, opens it
// and keeps it open.
//
// What thp measures is what it wrote downstream, and downstream is
// ingress-nginx with proxy buffering, not the viewer: a viewer on a link
// slower than their plan still has the limiter pacing thp until that buffer
// fills. So nothing here claims the plan is what the viewer's speed is bound
// by — only that the node is sending at the plan's cap.

// sessionStatsSources are the export items whose URL points at thp on the
// torrent's node, in order of preference: the stat URL (always the standard
// domain), then what a cached torrent still has — rest-api omits the stat
// item exactly when the content is cached, which is where the plan is most
// often the only limit. The export is asked with use-premium-domain=false
// (tryConnectStats), so these are standard-domain URLs too: the premium edge
// buffers an event stream.
var sessionStatsSources = []string{
	string(ra.ExportTypeTorrentStat),
	string(ra.ExportTypeDownload),
	string(ra.ExportTypeStream),
}

// sessionTarget is where the session stream is: base is the URL without a
// query, apiKey the key the export URLs carry for this deployment's thp.
type sessionTarget struct {
	base   string
	apiKey string
}

func (t sessionTarget) ok() bool { return t.base != "" }

// url is the stream's URL with a token. The export URLs' own token is never
// reused: it reached the browser in every link of the page, and thp does not
// open this stream for it.
func (t sessionTarget) url(token string) string {
	q := url.Values{}
	if t.apiKey != "" {
		q.Set("api-key", t.apiKey)
	}
	q.Set("token", token)
	return t.base + "?" + q.Encode()
}

// sessionStatsTarget derives the stream's location from an export response
// rest-api returned for this torrent: scheme and host of the first usable
// item, its path up to the infohash (a deployment path prefix included) and
// then session-stats/<infohash>; the api-key from its query. rest-api picks
// the node by rendezvous over the infohash, and that node's thp holds the
// counters, so the host is taken, never built. Zero when no item is a URL
// about this torrent.
func sessionStatsTarget(e *ra.ExportResponse, infohash string) sessionTarget {
	if e == nil || infohash == "" {
		return sessionTarget{}
	}
	for _, k := range sessionStatsSources {
		u, err := url.Parse(e.ExportItems[k].URL)
		if err != nil {
			continue
		}
		// Also skips an absent or empty item: no path, no infohash in it.
		i := strings.Index(u.Path, "/"+infohash)
		if i < 0 {
			continue
		}
		key := u.Query().Get("api-key")
		u.Path = u.Path[:i+1] + "session-stats/" + infohash
		u.RawPath, u.RawQuery, u.Fragment = "", "", ""
		return sessionTarget{base: u.String(), apiKey: key}
	}
	return sessionTarget{}
}

// sessionTokenTTL: the token only has to open the stream; the next stream
// gets a new one. Short, so one that leaks into a log is soon worth nothing.
// thp ends every stream at its token's expiry (handleSessionStats), so this
// is also how long one stream lives: the watch opens the next one
// sessionRotateLead before that (rotate). Variables for the stream-level
// test, which cannot wait ten minutes.
var (
	sessionTokenTTL    = 10 * time.Minute
	sessionRotateLead  = 30 * time.Second
	sessionExpirySlack = 5 * time.Second
)

// sessionToken is a minted token and the moment thp ends the stream it opens.
type sessionToken struct {
	tok string
	exp time.Time
}

// sessionStatsToken mints the stream's token, server side and never sent to
// a browser: the viewer's own claims — the ones rest-api signs into every
// export URL of this page, so thp keys the counters by the same sessionID and
// domain as the content — bound to this torrent (hash, lower case) and short
// lived. Everything else about it is the standard token, rate claim included:
// used for content by whoever got hold of it, it is no more than the page's
// own export links already are. exp is the expiry as signed (whole seconds).
func sessionStatsToken(sign func(jwt.Claims) (string, error), cl *api.Claims, infohash string, now time.Time) (sessionToken, error) {
	if cl == nil {
		return sessionToken{}, errors.New("no claims")
	}
	if cl.SessionID == "" {
		// thp keys the counters by session: without one there is
		// nothing to read, and asking is a guaranteed 403.
		return sessionToken{}, errors.New("no session")
	}
	t := *cl
	t.Hash = strings.ToLower(infohash)
	t.Rules = nil
	exp := jwt.NewNumericDate(now.Add(sessionTokenTTL))
	t.RegisteredClaims = jwt.RegisteredClaims{ExpiresAt: exp}
	tok, err := sign(&t)
	if err != nil {
		return sessionToken{}, err
	}
	return sessionToken{tok: tok, exp: exp.Time}, nil
}

const (
	// sessionRetries bounds the reopen attempts per failure run (about 2,
	// 4, 8 s): a thp pod rotation is worth riding out, a dead endpoint is
	// not worth a request every few seconds for as long as the page is open.
	sessionRetries = 3
	// sessionRetryReset: a reopened stream that has delivered events this
	// long is healthy again, and the next loss gets the whole budget — a
	// page open through three routine rollouts is not a dead endpoint.
	sessionRetryReset = 60 * time.Second
)

// errFinal marks a failure asking again cannot fix.
var errFinal = errors.New("final")

// sessionRetryable: a 4xx is thp's final answer — a thp without the route, a
// token without a session, a refused one — and asking again gets the same;
// so is a token we could not mint. Transport errors, 5xx and a stream that
// ended may be a pod going away.
func sessionRetryable(err error) bool {
	if errors.Is(err, errFinal) {
		return false
	}
	var se *api.StatusError
	if errors.As(err, &se) {
		return se.Status >= 500
	}
	return true
}

// retryDelay is the backoff for the n-th reopen (1-based): 2^n seconds times
// a factor in [0.5, 1.5). A thp rotation or an ingress reload cuts every
// stream on a node at once; without the spread they would all come back in
// the same millisecond.
func retryDelay(n int, jitter float64) time.Duration {
	return time.Duration(float64(time.Duration(1<<uint(n))*time.Second) * (0.5 + jitter))
}

type sessionOpener func(ctx context.Context, u string) (<-chan api.SessionStatsData, error)

// afterFunc schedules f and returns its stop — time.AfterFunc in production,
// a hand-driven clock in tests.
type afterFunc func(d time.Duration, f func()) (stop func() bool)

func realAfter(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

// openKind is why a stream is being opened, which decides what the meter
// keeps of the one before it.
type openKind int

const (
	// openFresh: the first stream, or a reopen after a failure — the gap
	// is unknown, and the meter starts over.
	openFresh openKind = iota
	// openRotation: the next stream, opened while the current one still
	// delivers, before thp ends it at its token's expiry. It takes over the
	// moment it opens; the meter only skips its first event.
	openRotation
	// openPlanned: thp ended the stream at its token's expiry with no
	// rotation to take over (it failed): reopened at once, off the retry
	// budget, keeping what was measured.
	openPlanned
)

type sessionOpen struct {
	ch   <-chan api.SessionStatsData
	err  error
	kind openKind
	// exp: thp ends this stream then. cancel ends it sooner (a rotation
	// replacing it); called on every path that does not keep it.
	exp    time.Time
	cancel context.CancelFunc
}

// sessionWatch follows one viewer's /session-stats stream for statusLoop.
// statusLoop owns it: every method but dial's goroutine runs on the loop's
// goroutine, which selects on results and ch. Timers (after) only start a
// dial, which touches nothing but what start() set before the first one.
type sessionWatch struct {
	rid    string
	open   sessionOpener
	after  afterFunc
	jitter func() float64
	// mint makes a fresh token for each open: every stream ends at its own
	// token's expiry.
	mint    func() (sessionToken, error)
	target  sessionTarget
	results chan sessionOpen
	ch      <-chan api.SessionStatsData
	// cancel ends the stream now open; exp is when thp ends it.
	cancel  context.CancelFunc
	exp     time.Time
	retries int
	// streamFrom is when the stream now open (or its rotated successors)
	// began delivering; zero before its first event, and again after a
	// failure.
	streamFrom time.Time
	stopRetry  func() bool
	// rotateAt is when the rotation of the stream now open is due, zero
	// when none is scheduled or it has reported; rotationFailed: it failed,
	// and the stream is reopened when it ends at exp; awaitRotation: the
	// stream ended while the rotation was under way, whose stream takes
	// over.
	rotateAt       time.Time
	stopRotate     func() bool
	rotationFailed bool
	awaitRotation  bool
	// tried: start was called (the export said where thp is, or that it
	// could not); gaveUp: the last answer was final, or the retries ran out.
	tried  bool
	gaveUp bool
	// lost: the stream ended and another is on its way (a rotation, a
	// planned reopen, a retry), until a reading comes again: the viewer's
	// link is in a gap, not known to be gone (reconnecting).
	lost  bool
	meter statusview.Meter
}

func newSessionWatch(rid string, open sessionOpener, after afterFunc, mint func() (sessionToken, error)) *sessionWatch {
	return &sessionWatch{rid: rid, open: open, after: after, mint: mint, jitter: rand.Float64, results: make(chan sessionOpen, 1)}
}

func (w *sessionWatch) started() bool { return w.target.ok() }

// dead: no reading can come on this status stream any more — the export
// named no thp to ask, thp's answer was final (an old thp without the route,
// a viewer without a session), or the retries ran out. Nothing revives it.
func (w *sessionWatch) dead() bool { return w.tried && (!w.started() || w.gaveUp) }

// start opens the stream once per status stream; later calls (a stats
// reconnect hands the target again) and an empty target do nothing.
func (w *sessionWatch) start(ctx context.Context, t sessionTarget) {
	w.tried = true
	if !t.ok() || w.started() {
		return
	}
	w.target = t
	w.dial(ctx, openFresh)
}

// dial opens a stream off the loop and reports on results. A result nobody
// will read (the status stream ended) is dropped, its stream cancelled.
func (w *sessionWatch) dial(ctx context.Context, kind openKind) {
	go func() {
		sctx, cancel := context.WithCancel(ctx)
		r := sessionOpen{kind: kind, cancel: cancel}
		if tok, err := w.mint(); err != nil {
			r.err = errors.Join(errFinal, err)
		} else {
			r.exp = tok.exp
			r.ch, r.err = w.open(sctx, w.target.url(tok.tok))
		}
		if r.err != nil {
			cancel()
		}
		select {
		case w.results <- r:
		case <-ctx.Done():
			cancel()
		}
	}()
}

func (w *sessionWatch) opened(ctx context.Context, r sessionOpen) {
	if r.kind == openRotation {
		w.rotateAt = time.Time{}
	}
	if r.err != nil {
		if r.kind == openRotation && !w.awaitRotation {
			// The stream it was to replace still delivers; it is reopened
			// when thp ends it (event).
			w.rotationFailed = true
			log.WithField("resourceID", w.rid).WithError(r.err).Debug("status: session stats rotation failed")
			return
		}
		w.awaitRotation = false
		w.retry(ctx, r.err)
		return
	}
	if r.kind == openRotation && w.ch == nil && !w.awaitRotation {
		// The stream it was to replace failed first and a reopen is on its
		// way: one stream is enough.
		r.cancel()
		return
	}
	if w.cancel != nil {
		// The rotation's predecessor: the loop no longer reads it.
		w.cancel()
	}
	w.ch, w.cancel, w.exp = r.ch, r.cancel, r.exp
	w.awaitRotation, w.rotationFailed = false, false
	if r.kind == openFresh {
		w.meter.StreamOpened()
		w.streamFrom = time.Time{}
	} else {
		w.meter.StreamRotated()
	}
	w.scheduleRotation(ctx)
}

// scheduleRotation opens the next stream sessionRotateLead before thp ends
// this one at its token's expiry: a planned end is not a failure, and must
// not cost the viewer's link or the plan's verdict (opened, StreamRotated).
func (w *sessionWatch) scheduleRotation(ctx context.Context) {
	w.stopRotation()
	if w.exp.IsZero() {
		return
	}
	w.rotateAt = w.exp.Add(-sessionRotateLead)
	w.stopRotate = w.after(time.Until(w.rotateAt), func() {
		if ctx.Err() == nil {
			w.dial(ctx, openRotation)
		}
	})
}

func (w *sessionWatch) stopRotation() {
	if w.stopRotate != nil {
		w.stopRotate()
		w.stopRotate = nil
	}
	w.rotateAt = time.Time{}
}

// event folds one read from ch into the watch; ok=false is the stream ending.
func (w *sessionWatch) event(ctx context.Context, ev api.SessionStatsData, ok bool, now time.Time) {
	if !ok {
		w.ended(ctx, now)
		return
	}
	if w.streamFrom.IsZero() {
		w.streamFrom = now
	}
	if w.retries > 0 && now.Sub(w.streamFrom) >= sessionRetryReset {
		w.retries = 0
	}
	w.meter.Observe(statusview.Sample{WindowSec: ev.WindowSec, BytesPerSec: ev.BytesPerSec, Conns: ev.Conns, Active: ev.Active, Rate: ev.Rate, Throttled: ev.Throttled}, now)
	if w.lost && w.meter.Reading(now).Known {
		w.lost = false
	}
}

// ended: the stream closed. Inside its last sessionRotateLead a rotation is
// under way and its stream takes over; at the token's expiry with none (it
// failed) thp simply ended it as planned, and it is reopened at once, off
// the retry budget; anything else is a failure.
func (w *sessionWatch) ended(ctx context.Context, now time.Time) {
	w.ch = nil
	w.lost = true
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	l := log.WithField("resourceID", w.rid)
	switch {
	case !w.rotateAt.IsZero() && !now.Before(w.rotateAt) && !w.rotationFailed:
		w.awaitRotation = true
		return
	case !w.exp.IsZero() && !now.Before(w.exp.Add(-sessionExpirySlack)):
		w.stopRotation()
		w.rotationFailed = false
		l.Debug("status: session stats token expired, reopening")
		w.dial(ctx, openPlanned)
		return
	}
	w.stopRotation()
	w.streamFrom = time.Time{}
	w.retry(ctx, nil)
}

// retry schedules the next open with backoff, or gives up — silently for the
// viewer: their link is then simply not drawn. err is nil for a stream that
// ended.
func (w *sessionWatch) retry(ctx context.Context, err error) {
	l := log.WithField("resourceID", w.rid).WithField("attempt", w.retries)
	if err != nil {
		l = l.WithError(err)
	}
	if err != nil && !sessionRetryable(err) {
		// A thp without the route, a viewer without a session: an ordinary
		// answer on many streams, not news for the log.
		w.gaveUp = true
		l.Debug("status: session stats unavailable")
		return
	}
	if w.retries >= sessionRetries {
		w.gaveUp = true
		l.Info("status: session stats unavailable, giving up")
		return
	}
	w.retries++
	delay := retryDelay(w.retries, w.jitter())
	l.WithField("in", delay).Info("status: session stats stream lost, reopening")
	w.stopRetry = w.after(delay, func() {
		if ctx.Err() == nil {
			w.dial(ctx, openFresh)
		}
	})
}

// reading is what the chain may say about the viewer at now.
func (w *sessionWatch) reading(now time.Time) statusview.Viewer {
	return w.meter.Reading(now)
}

// reconnecting: the stream that gave the readings ended and another is on
// its way -- neither a final answer nor the retries spent -- and has not
// read anything yet. An unknown reading then is a gap (statusview.Hold
// .Viewer); an open stream gone silent is not one, and neither is a stream
// that never delivered.
func (w *sessionWatch) reconnecting() bool {
	return w.lost && !w.gaveUp
}

// stop cancels a pending reopen or rotation and the stream now open.
func (w *sessionWatch) stop() {
	if w.stopRetry != nil {
		w.stopRetry()
	}
	w.stopRotation()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}
