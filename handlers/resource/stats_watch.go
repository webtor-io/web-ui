package resource

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/ratemeter"
	vault "github.com/webtor-io/web-ui/services/vault"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// statsWatch follows the seeder's stats stream (thp ?stats) for statusLoop:
// it connects (tryConnectStats), folds the frames into what the status says
// about the torrent, and reopens the stream when it closes and something is
// left to watch. statusLoop owns it: every method but dial's goroutine runs
// on the loop's goroutine, which selects on results and ch. Timers (after)
// only start a dial.
type statsWatch struct {
	rid     string
	connect func(ctx context.Context) statsConn
	after   afterFunc
	jitter  func() float64
	results chan statsConn
	// ch is the stream now open; nil while there is none.
	ch <-chan api.EventData

	last          *TorrentStatsData
	pieces        pieceMap
	rate          *ratemeter.Meter
	lastCompleted int
	// lastProgressAt is zero until Completed first grows on this stream;
	// firstStatsAt starts the observation window.
	lastProgressAt, firstStatsAt time.Time
	// liveSince is when this stream's frames turned live (zero while cold).
	liveSince time.Time
	// stale: the stream closed and a reconnect is pending. The last known
	// status keeps being shown (a frozen 51% beats a false "idle"), without
	// speed and without the paused/no-seeders verdicts — we do not know.
	// Seeder pods are rotated on every deploy, which closes every stream
	// they held; the download continues on the new pod.
	stale      bool
	reconnects int
	// unavailable: the stats connection failed for a reason other than
	// "cached". Rendering that as idle made an upstream 429 or 5xx look like
	// a dead torrent; "unknown" says what we actually know — nothing.
	unavailable bool
	stopRetry   func() bool
}

func newStatsWatch(rid string, connect func(ctx context.Context) statsConn, after afterFunc) *statsWatch {
	return &statsWatch{rid: rid, connect: connect, after: after, jitter: rand.Float64, results: make(chan statsConn, 1), rate: ratemeter.New(0.4)}
}

// dial connects off the loop and reports on results. A result nobody will
// read (the status stream ended) is dropped; its stream ends with ctx.
func (w *statsWatch) dial(ctx context.Context) {
	go func() {
		res := w.connect(ctx)
		select {
		case w.results <- res:
		case <-ctx.Done():
		}
	}()
}

// result folds a connection attempt into the watch.
func (w *statsWatch) result(ctx context.Context, res statsConn, now time.Time) {
	w.ch = res.ch
	log.WithField("resourceID", w.rid).WithField("connected", res.ch != nil).WithField("msg", res.msg).Info("status: stats connection result")
	if res.ch != nil {
		w.stale = false
		w.liveSince = time.Time{}
	}
	// If export says content is cached (no torrent_client_stat), mark as cached
	if res.msg == "cached" {
		w.last = &TorrentStatsData{Total: 1, Completed: 1, Seeders: 0}
	} else if res.ch == nil {
		if w.stale && shouldReconnect(w.last, w.reconnects, sinceProgress(w.lastProgressAt, now)) {
			w.reconnect(ctx)
		} else {
			w.unavailable = true
			if w.stale {
				// Retries exhausted: stop pretending to know.
				w.last = nil
				w.stale = false
			}
		}
	}
}

// frame folds one stats event into the watch; false: there is nothing to
// send for it.
func (w *statsWatch) frame(ev api.EventData, now time.Time) bool {
	if ev.Status == api.StatTerminated {
		// The seeder pod is going away and says so with every counter
		// zero; the stream ends right after. Read as stats, it was a
		// torrent with nothing stored: "idle", and the close then
		// reconnected to nothing (shouldReconnect wants something stored)
		// -- a deploy mid-download read "idle" for good. Skipped, the close
		// goes through shouldReconnect with the real progress, and the new
		// pod picks the download up.
		log.WithField("resourceID", w.rid).Debug("status: seeder terminating")
		return false
	}
	w.pieces.apply(ev)
	fill, active := w.pieces.buckets()
	// Completed is verified bytes; its delta per second is the swarm's
	// useful throughput — the download speed a torrent client would show.
	rps := w.rate.Sample(int64(ev.Completed), now)
	if w.firstStatsAt.IsZero() {
		w.firstStatsAt = now
		w.lastCompleted = ev.Completed
	} else if ev.Completed != w.lastCompleted {
		w.lastCompleted = ev.Completed
		w.lastProgressAt = now
	}
	live := ev.Live == nil || *ev.Live
	if !live {
		w.liveSince = time.Time{}
	} else if w.liveSince.IsZero() {
		w.liveSince = now
	}
	w.last = &TorrentStatsData{
		Live:              live,
		Rate:              rps,
		Total:             ev.Total,
		Completed:         ev.Completed,
		Seeders:           ev.Seeders,
		Leechers:          ev.Leechers,
		Peers:             ev.Peers,
		Fill:              fill,
		Active:            active,
		PiecesDone:        w.pieces.done(),
		PiecesTotal:       len(w.pieces.complete),
		Holes:             w.pieces.holes(),
		AvailabilityKnown: ev.AvailabilityKnown,
		Availability:      ev.Availability,
		WantedMissing:     ev.WantedMissing,
		ReaderMissing:     ev.ReaderMissing,
	}
	log.WithField("resourceID", w.rid).WithField("completed", ev.Completed).WithField("total", ev.Total).WithField("peers", ev.Peers).WithField("seeders", ev.Seeders).WithField("leechers", ev.Leechers).Debug("status: got stats event")
	return true
}

// closed: the stream ended — seeder gone or connection dropped. The last
// status stays on screen while a reconnect is due; it is forgotten only
// when there is nothing worth reconnecting for.
func (w *statsWatch) closed(ctx context.Context, now time.Time) {
	log.WithField("resourceID", w.rid).Warn("status: stats channel closed")
	w.ch = nil
	switch {
	case shouldReconnect(w.last, w.reconnects, sinceProgress(w.lastProgressAt, now)):
		w.stale = true
		w.reconnect(ctx)
	case w.last != nil && w.last.whole():
		// The seeder closes the stream once the torrent is complete:
		// nothing is left to reconnect for, and nothing to forget either
		// -- the torrent is in the cache. Forgotten, it read "idle"
		// ("Webtor ожидает" on 5461f58a…, 2026-09-25). Nothing moves on
		// it any more.
		w.last.Rate, w.last.Active = 0, nil
	default:
		w.last = nil
	}
}

// reconnect schedules the next connection with backoff (retryDelay: a
// seeder rollout closes every stream of a pod at once, and they must not
// all come back in the same millisecond).
func (w *statsWatch) reconnect(ctx context.Context) {
	w.reconnects++
	delay := retryDelay(w.reconnects, w.jitter())
	log.WithField("resourceID", w.rid).WithField("attempt", w.reconnects).WithField("in", delay).Info("status: stats stream closed mid-download, reconnecting")
	w.stopRetry = w.after(delay, func() {
		if ctx.Err() == nil {
			w.dial(ctx)
		}
	})
}

// tick re-samples the meter with the unchanged counter: the seeder only
// sends events when something changed, so a swarm that stopped sends
// nothing, and the speed has to decay instead of freezing at the last
// value it had when the bytes stopped.
func (w *statsWatch) tick(now time.Time) {
	if w.last != nil && w.ch != nil {
		w.last.Rate = w.rate.Sample(int64(w.lastCompleted), now)
	}
}

// status is the torrent's status at now: Vault's word (db, apiRes) over
// what the stream says, with the caching verdicts. settling is the
// stream's half of it -- the view's hold has the other (statusLoop).
func (w *statsWatch) status(db *vaultModels.Resource, apiRes *vault.Resource, now time.Time) *TorrentStatus {
	if w.last != nil {
		// Zero over a closed stream too: we do not know, as for the
		// caching verdicts below.
		w.last.LiveFor = 0
		if !w.liveSince.IsZero() && !w.stale {
			w.last.LiveFor = now.Sub(w.liveSince)
		}
	}
	status := resolveStatus(db, apiRes, w.last)
	if status.State == "idle" && w.unavailable {
		status.State = "unknown"
		status.withBarPolicy()
	}
	if w.last != nil && !w.stale && !w.firstStatsAt.IsZero() {
		activity := hasActive(w.last.Active) || (!w.lastProgressAt.IsZero() && now.Sub(w.lastProgressAt) < settleAfter)
		switch judgeSwarm(status.State, now.Sub(w.firstStatsAt), activity, w.last.Live, w.last.Seeders, w.last.Peers) {
		case verdictChecking:
			status.Checking = true
		case verdictPaused:
			status.Paused = true
		case verdictNoSeeders:
			status.NoSeeders = true
		}
	}
	// A speed that reads as zero is zero: paused means nothing moves, and
	// below half a kilobyte the smoothed tail is noise, not throughput.
	if w.stale || status.Paused || status.NoSeeders || status.Checking || status.Rate < 512 {
		status.Rate = 0
	}
	// The chain's swarm moves while its bytes arrive, not while the
	// smoothed rate is still decaying from them (movingFor).
	status.swarmStill = w.lastProgressAt.IsZero() || now.Sub(w.lastProgressAt) >= movingFor
	// The first piece can arrive before the rate meter has an interval to
	// measure it over: settled once the swarm moved, or the window is over.
	status.settling = !w.firstStatsAt.IsZero() && now.Sub(w.firstStatsAt) < settleAfter
	return status
}

// stop cancels a pending reconnect.
func (w *statsWatch) stop() {
	if w.stopRetry != nil {
		w.stopRetry()
	}
}

// statsConn is one attempt at the stats stream: the stream (nil when not
// connected), why not ("cached", or what failed), where the viewer's
// /session-stats stream is (from the same export response; zero when there
// was none) and the size the download ETA prices (0 unknown).
type statsConn struct {
	ch      <-chan api.EventData
	msg     string
	session sessionTarget
	size    int64
}

// tryConnectStats attempts to establish an SSE connection to the torrent-http-proxy
// for real-time torrent-level stats. Gets the root content ID from the list response,
// then uses ExportResourceContent to get the stat URL for the whole torrent. The
// same export response yields the viewer's /session-stats location — no extra
// call; it is asked for standard-domain URLs, since the premium edge buffers
// an event stream. file is the page's path, whose size prices the ETA.
func (s *Handler) tryConnectStats(ctx context.Context, claims *api.Claims, resourceID string, file string) statsConn {
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Get root content ID from list response
	list, err := s.api.ListResourceContentCached(connCtx, claims, resourceID, &api.ListResourceContentArgs{
		Output: api.OutputList,
		Limit:  1,
	})
	if err != nil {
		msg := fmt.Sprintf("list failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return statsConn{msg: msg}
	}

	// Use root item ID (ListResponse embeds ListItem with ID)
	rootID := list.ID
	if rootID == "" {
		return statsConn{msg: "empty root ID"}
	}
	size := s.priceSize(connCtx, claims, resourceID, file, list.Size)

	// Get torrent-level export using root content ID
	exportResp, err := s.api.ExportResourceContentStandardDomain(connCtx, claims, resourceID, rootID)
	if err != nil {
		msg := fmt.Sprintf("export failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return statsConn{msg: msg, size: size}
	}
	sess := sessionStatsTarget(exportResp, resourceID)

	statItem, ok := exportResp.ExportItems["torrent_client_stat"]
	if !ok || statItem.URL == "" {
		// No stat URL means content is cached (rest-api skips torrent_client_stat for cached content)
		return statsConn{msg: "cached", session: sess, size: size}
	}

	// Check stats URL is accessible before opening SSE
	log.WithField("resourceID", resourceID).WithField("url", helpers.RedactURL(statItem.URL)).Info("status: connecting to stats SSE")

	// Open SSE connection to torrent-http-proxy (use parent ctx, not timeout ctx)
	ch, err := s.api.Stats(ctx, statItem.URL)
	if err != nil {
		// 404 from seeder means content is available (cached/vaulted)
		if err.Error() == "cached" {
			return statsConn{msg: "cached", session: sess, size: size}
		}
		msg := fmt.Sprintf("stats SSE failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return statsConn{msg: msg, session: sess, size: size}
	}
	log.WithField("resourceID", resourceID).Info("status: connected to torrent stats SSE")
	return statsConn{ch: ch, msg: "connected", session: sess, size: size}
}

// priceSize is the size the download ETA prices: the page's file (or folder)
// when it names one rest-api knows, else the whole torrent. A failed lookup
// only costs the ETA its precision.
func (s *Handler) priceSize(ctx context.Context, claims *api.Claims, resourceID, file string, rootSize int64) int64 {
	if file == "" || file == "/" {
		return rootSize
	}
	l, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{Path: file, Output: api.OutputList, Limit: 1})
	if err != nil || l == nil || l.Size <= 0 {
		return rootSize
	}
	return l.Size
}
