package resource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	csrf "github.com/utrack/gin-csrf"
	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	uclaims "github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/ratemeter"
	"github.com/webtor-io/web-ui/services/statusview"
	vault "github.com/webtor-io/web-ui/services/vault"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// TorrentStatus represents the current combined status of a torrent.
type TorrentStatus struct {
	State    string  `json:"state"`    // idle, caching, cached, vaulting, vaulted, unknown
	Progress float64 `json:"progress"` // 0-100 for caching/vaulting
	Seeders  int     `json:"seeders"`  // seeders as the seeder reports them
	Leechers int     `json:"leechers"` // leechers as the seeder reports them
	Peers    int     `json:"peers"`    // combined peer count — the fallback when a seeder reports no split
	Label    string  `json:"label"`    // translated state label
	// Swarm is the translated "N seeders · M leechers" (or "N peers") suffix,
	// empty when nothing is known. Formatted server-side so the JS renderer
	// never has to carry locale strings.
	Swarm string `json:"swarm"`
	// Pieces is the piece bar: PieceBuckets bytes, base64, one per bucket,
	// 0..255 = share of the bucket's pieces the seeder holds. Active is a
	// base64 bitset of buckets with pieces the seeder is fetching right now.
	// Empty when nothing is known and for complete content (see barStates).
	Pieces string `json:"pieces,omitempty"`
	Active string `json:"active,omitempty"`
	// Missing is a base64 bitset of the buckets holding a piece nobody
	// connected has and we do not have either -- the bar hatches them
	// (pieceMap.holes). Only once the seeder knows (availability_known), and
	// only while the bar is drawn.
	Missing     string `json:"missing,omitempty"`
	PiecesDone  int    `json:"pieces_done,omitempty"`
	PiecesTotal int    `json:"pieces_total,omitempty"`
	PiecesLabel string `json:"pieces_label,omitempty"`
	// Rate is the swarm's useful download throughput in bytes per second,
	// smoothed (services/ratemeter) from the seeder's Completed counter;
	// RateLabel is it formatted ("2.3 MB/s"). Zero/empty when unknown or
	// when nothing is moving.
	Rate      float64 `json:"rate,omitempty"`
	RateLabel string  `json:"rate_label,omitempty"`
	// Paused: caching, but nothing is being fetched — no verified bytes
	// arrived for pausedAfter and no piece is queued. The seeder downloads
	// on demand, so this means "nobody is streaming this right now", not
	// "stuck"; the badge turns amber with a pause glyph to say so.
	Paused     bool   `json:"paused,omitempty"`
	PausedHint string `json:"paused_hint,omitempty"`
	// NoSeeders: caching, and the seeder sees nobody at all — the download
	// cannot progress until a seeder shows up. Takes precedence over Paused
	// in the badge: an empty swarm is the fact that matters.
	NoSeeders     bool   `json:"no_seeders,omitempty"`
	NoSeedersHint string `json:"no_seeders_hint,omitempty"`
	// Checking: caching with partial content, but we have watched the swarm
	// for less than the settle window and seen no activity yet — too early to
	// call it caching, paused or dead. The badge stays neutral until the
	// verdict is earned.
	Checking bool `json:"checking,omitempty"`
	// View is the transfer status as the resource page draws it — the
	// chain "swarm ▸ cache ▸ you", the bar's mode, the hint or the plan
	// box, the details popover — built by services/statusview and already
	// localized. Only on the resource page's stream (?session=1), which
	// also follows the viewer's own thp session; the Vault dashboard's rows
	// read state, progress and Badge.
	View *statusview.View `json:"view,omitempty"`
	// Badge is the view's badge alone, built the same way with no viewer
	// (present): what the Vault dashboard's rows draw, in the very element
	// the resource page's badge is (partials/status/badge.html,
	// lib/statusBadge.js). Only on the dashboard's stream (no ?session=1);
	// the page's has it in View.
	Badge *statusview.Badge `json:"badge,omitempty"`
	// Final: the last message of this stream. Nothing on it can change any
	// more — a vaulted torrent whose viewer's link cannot be followed (no
	// thp to ask, a final answer, the retries spent) — so the server ends
	// it, and the page closes its EventSource on this rather than letting it
	// reconnect to the same answer every few seconds.
	Final bool `json:"final,omitempty"`
	// swarmKnown: the seeder reported the swarm (withSwarm); zero seeders
	// then mean zero, not "not asked".
	swarmKnown bool
	// The seeder's availability for the view (statusview.Torrent): not sent
	// as such -- the view says what it means.
	availKnown    bool
	availability  float64
	holes         bool
	wantedMissing int
	readerMissing int
	// cachePct is the share of the torrent in the cache, 0..100, from the
	// seeder's numbers -- a Vault state's own Progress is Vault's.
	cachePct float64
	// swarmStill: none of the swarm's bytes arrived within movingFor (the
	// status loop's word). The view then gets no swarm rate: the smoothed
	// one outlives the bytes by seconds, decaying a tick at a time.
	swarmStill bool
	// settling: the stream has watched the swarm for less than settleAfter
	// and not seen it move yet (statusview.Torrent.Settling).
	settling bool
}

// movingFor: the swarm moves while its bytes arrive -- Completed grew at most
// this long ago, which is the status sent on the stats event itself (and a
// tick close enough to it that the rate meter has not re-sampled: it waits
// half a second between samples). The seeder's counter grows a verified
// piece at a time, so between two pieces nothing arrives for a while; the
// view's hold (statusview.Hold) keeps the swarm on the chain as it last
// moved through such a gap, for HoldFor from its last piece. Judged by the
// smoothed rate instead, the swarm "moved" for 13 s after its last byte at
// 38 Mbps (the rate decays by 0.6 a tick), and the hold came on top of that:
// the badge 14-22 s after the last byte, and a chain showing a pause or a
// dash for the last ten of them.
const movingFor = 500 * time.Millisecond

// settleAfter is the observation window before any verdict: a piece boundary
// can make a live download look still for a second; five seconds of no
// progress is a pause. noSeedersAfter is the much longer window before
// "no seeders": a seeder pod that has just started sees an empty swarm for
// tens of seconds while it reaches trackers and the DHT (16 s to the first
// peer on a8386ee1, 2026-09-03), and calling that "no seeders" at five
// seconds was simply wrong. Until then an idle torrent is what it looks like:
// paused. The badge does not wait out the long window in "checking" — that
// read as a stuck spinner.
const (
	settleAfter    = 5 * time.Second
	noSeedersAfter = 30 * time.Second
)

// swarmVerdict is the caching badge's face once the window has passed.
type swarmVerdict int

const (
	verdictChecking  swarmVerdict = iota // window not over, no activity seen yet
	verdictCaching                       // progress or queued pieces — alive
	verdictPaused                        // no activity; nothing is moving
	verdictNoSeeders                     // no activity and the swarm stayed empty
)

// judgeSwarm is the whole decision, pure so it can be tested. observed is how
// long we have watched this stream; activity is progress within settleAfter
// or a piece queued for fetching; seeders/peers are the seeder's counts.
//
// Activity at any moment → caching. No activity: checking until settleAfter,
// paused after — whoever is or is not around, nothing moves. Nobody around
// for the whole of noSeedersAfter → no seeders: the swarm got its time to
// appear before we say it is gone.
func judgeSwarm(state string, observed time.Duration, activity bool, live bool, seeders, peers int) swarmVerdict {
	if state != "caching" || activity {
		return verdictCaching
	}
	// A cold reply (the seeder did not load the torrent, nobody is
	// streaming it) has no swarm by design: what is stored is paused, and
	// nothing can be said about seeders — neither "checking" nor "none".
	if !live {
		return verdictPaused
	}
	if observed < settleAfter {
		return verdictChecking
	}
	if seeders == 0 && peers == 0 && observed >= noSeedersAfter {
		return verdictNoSeeders
	}
	return verdictPaused
}

// recentActivity is how fresh the last progress must be for a closed stream
// to count as "a download interrupted", not "a torrent the seeder unloaded".
const recentActivity = 60 * time.Second

// shouldReconnect decides whether a closed stats stream is worth reopening:
// only while a download was actually in progress — something stored, not all
// of it, and bytes moving within recentActivity — and only a few times. The
// seeder unloads idle torrents on its own; a stream that closes after a quiet
// spell is that, and reopening it would start a seeder pod nobody asked for
// just to draw a badge. Such a torrent falls back to idle, as it always did
// -- unless it is complete: the seeder closes the stream at 100% too, and
// that one stays cached (statusLoop).
func shouldReconnect(stats *TorrentStatsData, attempts int, sinceProgress time.Duration) bool {
	if stats == nil || attempts >= 5 {
		return false
	}
	if stats.Completed <= 0 || int64(stats.Completed) >= stats.Total {
		return false
	}
	return sinceProgress >= 0 && sinceProgress < recentActivity
}

// sinceProgress is the age of the last observed progress, or -1 when none was
// observed on this stream.
func sinceProgress(lastProgressAt time.Time) time.Duration {
	if lastProgressAt.IsZero() {
		return -1
	}
	return time.Since(lastProgressAt)
}

// hasActive reports whether any bit of the active bucket bitset is set.
func hasActive(active []byte) bool {
	for _, b := range active {
		if b != 0 {
			return true
		}
	}
	return false
}

// PieceBuckets is the piece bar's resolution: enough cells for any screen,
// ~350 bytes per update instead of a 10 000-piece array every second.
const PieceBuckets = 256

// TorrentStatsData holds the relevant fields from a torrent stats event.
type TorrentStatsData struct {
	Total     int64
	Completed int
	Seeders   int
	Leechers  int
	Peers     int
	// Fill/Active are the bucketed piece map (see pieceMap); nil until the
	// stream has told us about any piece. PiecesDone/PiecesTotal count
	// pieces, independent of what Total/Completed above measure.
	Fill        []byte
	Active      []byte
	PiecesDone  int
	PiecesTotal int
	Rate        float64
	// Live is false for a cold reply: the seeder read the numbers from
	// disk and did not join the swarm, so an empty swarm means nothing.
	Live bool
	// Holes is the bucketed bitset of pieces nobody connected has
	// (pieceMap.holes), nil when there are none or the seeder does not know;
	// the rest is the seeder's availability as the last frame said it
	// (api.EventData).
	Holes             []byte
	AvailabilityKnown bool
	Availability      float64
	WantedMissing     int
	ReaderMissing     int
}

// whole: every byte of the torrent is in the cache.
func (s *TorrentStatsData) whole() bool {
	return s.Total > 0 && int64(s.Completed) >= s.Total
}

// withSwarm copies the swarm counters and the piece bar from a stats event
// onto a status.
func (t *TorrentStatus) withSwarm(stats *TorrentStatsData) *TorrentStatus {
	if stats != nil {
		t.Seeders, t.Leechers, t.Peers = stats.Seeders, stats.Leechers, stats.Peers
		t.swarmKnown = true
		if len(stats.Fill) > 0 {
			t.Pieces = base64.StdEncoding.EncodeToString(stats.Fill)
			t.Active = base64.StdEncoding.EncodeToString(stats.Active)
			t.PiecesDone = stats.PiecesDone
			t.PiecesTotal = stats.PiecesTotal
		}
		t.Rate = stats.Rate
		if stats.Total > 0 {
			t.cachePct = float64(stats.Completed) / float64(stats.Total) * 100
		}
		// Nothing of the availability is read unless the seeder knows it:
		// before that its union is a lower bound, holes that are not there.
		if stats.AvailabilityKnown {
			t.availKnown, t.availability = true, stats.Availability
			t.wantedMissing, t.readerMissing = stats.WantedMissing, stats.ReaderMissing
			if hasActive(stats.Holes) {
				t.holes = true
				t.Missing = base64.StdEncoding.EncodeToString(stats.Holes)
			}
		}
	}
	return t
}

// barStates are the states in which the piece bar is drawn: the torrent is
// on its way -- caching, a Vault transfer, one that failed mid-way with
// pieces already stored, or Vault waiting for seeders over what the cache
// holds (the approved design, 2026-09-25, draws its purple bar). Complete
// content (cached, vaulted) draws nothing: a static full bar told the user
// nothing the badge did not. An idle seeder that merely knows its pieces
// draws nothing either -- it drew an empty bar that vanished when its stats
// channel closed -- unless pieces nobody connected has are the story: the
// bar is where they are hatched. Everything else shows the hairline
// divider.
var barStates = map[string]bool{"caching": true, "vaulting": true, "vault_failed": true, "vault_waiting": true}

// withBarPolicy strips the piece bar from states that must not show one.
func (t *TorrentStatus) withBarPolicy() *TorrentStatus {
	if !barStates[t.State] && !(t.State == "idle" && t.holes) {
		t.Pieces, t.Active, t.Missing, t.PiecesDone, t.PiecesTotal, t.PiecesLabel = "", "", "", 0, 0, ""
		t.Rate = 0
	}
	return t
}

// pieceMap is the seeder's piece state as this status stream knows it. The
// seeder sends the full list only in the first event of a stream and then
// just the pieces that changed (torrent-web-seeder Stat.StatStream → diff), so
// bucketing each event on its own drew a bar from a handful of pieces and then
// nothing — the map has to be kept and patched.
type pieceMap struct {
	complete []bool
	active   []bool
	// missing are the seeder's runs of pieces no connected peer has, kept
	// across frames the same way (api.EventData.Missing); availKnown is
	// the last frame's availability_known.
	missing    []api.PieceRange
	availKnown bool
}

// apply folds one stats event into the map. The first event sizes it (a
// stream always opens with the full list); positions past the end grow it,
// so a diff-only stream still builds a usable, if partial, picture. The
// missing runs are replaced by every frame that carries them -- null or []
// is "none" -- and kept by one that says they did not change.
func (m *pieceMap) apply(ev api.EventData) {
	m.availKnown = ev.AvailabilityKnown
	if !ev.MissingUnchanged {
		m.missing = append(m.missing[:0], ev.Missing...)
	}
	if len(ev.Pieces) == 0 {
		return
	}
	need := len(ev.Pieces)
	for _, p := range ev.Pieces {
		if p.Position+1 > need {
			need = p.Position + 1
		}
	}
	if need > len(m.complete) {
		grow := make([]bool, need)
		copy(grow, m.complete)
		m.complete = grow
		growA := make([]bool, need)
		copy(growA, m.active)
		m.active = growA
	}
	for _, p := range ev.Pieces {
		if p.Position < 0 {
			continue
		}
		m.complete[p.Position] = p.Complete
		m.active[p.Position] = !p.Complete && p.Priority > 0
	}
}

func (m *pieceMap) known() bool { return len(m.complete) > 0 }

// done counts complete pieces.
func (m *pieceMap) done() int {
	n := 0
	for _, c := range m.complete {
		if c {
			n++
		}
	}
	return n
}

// buckets folds the map into PieceBuckets cells: fill is the share of complete
// pieces in the cell (0..255); the active bit says the cell holds a piece being
// fetched. Torrents with fewer pieces than buckets get one cell per piece.
func (m *pieceMap) buckets() (fill, active []byte) {
	n := len(m.complete)
	if n == 0 {
		return nil, nil
	}
	cells := PieceBuckets
	if n < cells {
		cells = n
	}
	complete := make([]int, cells)
	total := make([]int, cells)
	active = make([]byte, (cells+7)/8)
	for pos := 0; pos < n; pos++ {
		c := pos * cells / n
		total[c]++
		if m.complete[pos] {
			complete[c]++
		} else if m.active[pos] {
			active[c/8] |= 1 << uint(c%8)
		}
	}
	fill = make([]byte, cells)
	for c := range fill {
		if total[c] > 0 {
			fill[c] = byte(complete[c] * 255 / total[c])
		}
	}
	return fill, active
}

// holes folds the missing runs into the same cells as buckets: a cell's bit
// is set when it holds a piece nobody connected has that is not complete
// here either -- past 512 runs the seeder merges them, so a run may cover
// complete pieces, and completion is drawn over the hatch. Runs past the map
// are clamped, never grown into. nil when there is nothing to hatch or the
// seeder does not know yet (availability_known: until the peers' bitfields
// are in, the union paints holes that are not there).
func (m *pieceMap) holes() []byte {
	n := len(m.complete)
	if !m.availKnown || n == 0 || len(m.missing) == 0 {
		return nil
	}
	cells := PieceBuckets
	if n < cells {
		cells = n
	}
	bits := make([]byte, (cells+7)/8)
	any := false
	for _, r := range m.missing {
		for pos := max(r.Start, 0); pos < min(r.End, n); pos++ {
			if m.complete[pos] {
				continue
			}
			c := pos * cells / n
			bits[c/8] |= 1 << uint(c%8)
			any = true
		}
	}
	if !any {
		return nil
	}
	return bits
}

// resolveStatus is a pure function that determines the combined torrent status
// from vault DB state, vault API state, and torrent seeding stats.
// Priority: vaulted > vaulting > cached > caching > idle.
func resolveStatus(dbResource *vaultModels.Resource, apiResource *vault.Resource, stats *TorrentStatsData) *TorrentStatus {
	return resolveStatusRaw(dbResource, apiResource, stats).withBarPolicy()
}

func resolveStatusRaw(dbResource *vaultModels.Resource, apiResource *vault.Resource, stats *TorrentStatsData) *TorrentStatus {
	vaultState := resolveVaultState(dbResource, apiResource)
	cachingState := resolveCachingState(stats)

	if vaultState.State == "vaulted" {
		return vaultState
	}
	if vaultState.State == "vaulting" || vaultState.State == "vault_failed" {
		vaultState.withSwarm(stats)
		// Funded, nothing stored, and the seeder sees nobody: the transfer is
		// not slow, it is waiting for a swarm that is not there. Saying so is
		// the difference between "stuck at 0%" and "no seeders yet". Not for
		// content whole in the cache: Vault takes it from there, no swarm
		// needed (a cached torrent's stats are the synthetic "cached" ones,
		// with nobody in them).
		if vaultState.State == "vaulting" && vaultState.Progress == 0 && stats != nil && stats.Seeders == 0 && stats.Peers == 0 && !stats.whole() {
			vaultState.State = "vault_waiting"
		}
		return vaultState
	}
	// Cached, caching, or idle -- idle with the swarm the seeder reports,
	// when it does: who is there ("Waiting (14 seeders)"), and whether they
	// have the pieces at all.
	return cachingState
}

func resolveVaultState(dbResource *vaultModels.Resource, apiResource *vault.Resource) *TorrentStatus {
	if dbResource == nil {
		return &TorrentStatus{State: "idle"}
	}
	if dbResource.Vaulted {
		return &TorrentStatus{State: "vaulted"}
	}
	if !dbResource.Funded {
		return &TorrentStatus{State: "idle"}
	}
	// Funded but not vaulted — check API for progress
	if apiResource == nil {
		return &TorrentStatus{State: "vaulting", Progress: 0}
	}
	switch apiResource.Status {
	case vault.StatusProcessing:
		return &TorrentStatus{State: "vaulting", Progress: apiResource.GetProgress()}
	case vault.StatusCompleted:
		return &TorrentStatus{State: "vaulted"}
	case vault.StatusQueued:
		return &TorrentStatus{State: "vaulting", Progress: 0}
	default:
		// Failed (or unknown): still funded and the system retries, but the
		// user deserves to know the last attempt did not go through — this
		// used to render as "Vaulting N%" forever. The API's error text is
		// not passed on: it is the Vault worker's raw error, which quotes
		// the URL it fetched (token and api-key included), and nothing on
		// the page shows it.
		return &TorrentStatus{State: "vault_failed", Progress: apiResource.GetProgress()}
	}
}

func resolveCachingState(stats *TorrentStatsData) *TorrentStatus {
	if stats == nil {
		return &TorrentStatus{State: "idle"}
	}
	if stats.Total <= 0 {
		return &TorrentStatus{State: "idle"}
	}
	// If nothing has been downloaded yet, treat as idle — don't show "Caching 0%"
	// which would be misleading (the seeder may have been started just by our stats probe)
	if stats.Completed <= 0 {
		return (&TorrentStatus{State: "idle"}).withSwarm(stats)
	}
	progress := float64(stats.Completed) / float64(stats.Total) * 100
	if progress >= 100 {
		return (&TorrentStatus{State: "cached", Progress: 100}).withSwarm(stats)
	}
	return (&TorrentStatus{State: "caching", Progress: progress}).withSwarm(stats)
}

// prepareInitialStatus computes the initial status for SSR (vault DB only, no SSE connection).
func (s *Handler) prepareInitialStatus(ctx context.Context, resourceID string) *TorrentStatus {
	if s.statusVault == nil {
		return &TorrentStatus{State: "idle"}
	}
	dbResource, err := s.statusVault.GetResource(ctx, resourceID)
	if err != nil {
		log.WithError(err).Warn("failed to get vault resource for initial status")
		return &TorrentStatus{State: "idle"}
	}
	return resolveStatus(dbResource, nil, nil)
}

// status is the SSE endpoint handler for real-time torrent status updates.
// Uses c.Stream() + c.SSEvent() like the job handler for proper proxy compatibility.
// All computation happens in a background goroutine; the callback only reads from a channel.
func (s *Handler) status(c *gin.Context) {
	// Validate CSRF token from query parameter (EventSource doesn't support custom headers)
	token := c.Query("_csrf")
	if token == "" || token != csrf.GetToken(c) {
		c.String(http.StatusForbidden, "CSRF token mismatch")
		return
	}

	resourceID := c.Param("resource_id")
	// The page hands the badge a short-lived token bound to the infohash
	// (torrent_link.go). CSRF alone was not enough: one harvested session
	// cookie + CSRF pair opened streams indefinitely from clients that never
	// loaded the (edge-challenged) page.
	if s.secret != "" {
		if err := CheckStatusToken(s.secret, c.Query("token"), resourceID, time.Now()); err != nil {
			c.String(http.StatusForbidden, "status token missing or expired")
			return
		}
	}
	claims := api.GetClaimsFromContext(c)
	// Read before the loop's goroutine starts: it must not touch c.
	env := s.viewEnv(c, claims)
	// The chain and the viewer's own link are asked for by the resource
	// page, which draws them (?session=1); the Vault dashboard's rows open
	// this stream too and would each hold a thp stream nobody looks at.
	env.withView = c.Query("session") == "1"
	// The file the page is on, whose size the download ETA prices; the
	// whole torrent without one.
	if f := c.Query("file"); len(f) <= maxFileParam {
		env.file = f
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Channel for status updates from background goroutine
	statusCh := make(chan *TorrentStatus, 10)

	if dbg := debugStatus(c, env); dbg != nil {
		go func() {
			defer close(statusCh)
			select {
			case statusCh <- dbg:
			case <-ctx.Done():
			}
			<-ctx.Done()
		}()
	} else {
		go s.statusLoop(ctx, claims, resourceID, statusCh, env)
	}

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(5 * time.Second)
		select {
		case <-ctx.Done():
			ticker.Stop()
			return false
		case <-ticker.C:
			c.SSEvent("ping", "")
			return true
		case status, ok := <-statusCh:
			if !ok {
				return false
			}
			// Localized and built in the loop (present), so its dedup
			// compares what the page would see.
			c.SSEvent("message", status)
			return !endsStream(status, env)
		}
	})
}

// maxFileParam bounds ?file: it is only forwarded to rest-api as a list path.
const maxFileParam = 4096

// endsStream: "vaulted" is final for the Vault dashboard's rows, which close
// on it. The resource page keeps listening while the viewer's own link can
// still change — vaulted content is served through thp like any other — and
// until the status is Final (statusLoop).
func endsStream(st *TorrentStatus, env *viewEnv) bool {
	return st.Final || (st.State == "vaulted" && !env.withView)
}

// viewEnv is what presenting a status needs about the request: read from the
// gin context once, in the handler, because the status loop runs on its own
// goroutine and must not touch the context.
type viewEnv struct {
	lang     string
	loc      *goi18n.Localizer
	tier     string
	signedIn bool
	// claimCap is the cap in the viewer's own claims, Mbps; 0 — none.
	claimCap float64
	offers   statusview.Offers
	// withView: the resource page's stream — build the view and follow the
	// viewer's thp session.
	withView bool
	file     string
	// debug: a debugStatus preview — the plan box falls back to a sample
	// offer on an instance without a catalog, so it can be reviewed.
	debug bool
	// hold keeps the swarm on this stream's chain through the gaps between
	// its pieces, and the viewer through a lost stream to thp
	// (statusview.Hold): the chain gives way to the badge statusview.HoldFor
	// after the swarm last moved -- and at once when the viewer leaves with
	// nothing else moving: they are on it while a request of theirs is open
	// (statusview.Viewer.Present), not while a speed is held.
	hold statusview.Hold
}

func (s *Handler) viewEnv(c *gin.Context, claims *api.Claims) *viewEnv {
	e := &viewEnv{
		lang:     i18n.GetLang(c),
		loc:      i18n.GetLocalizer(c),
		tier:     uclaims.GetFromContext(c).GetContext().GetTier().GetName(),
		signedIn: auth.GetUserFromContext(c).HasAuth(),
	}
	// Only a real catalog: a nil *offer.Service in the interface would be
	// a non-nil Offers.
	if s.offers != nil {
		e.offers = s.offers
	}
	if claims != nil {
		e.claimCap = statusview.RateMbps(claims.Rate)
	}
	return e
}

// sampleOffers stands in for an empty catalog in a debugStatus preview: the
// promo plan as production sells it, so the plan box can be looked at. Its
// /trial link is a 404 on such an instance — it is there to be seen.
type sampleOffers struct{ statusview.Offers }

func (o sampleOffers) Promo() *offer.Offer {
	if o.Offers != nil {
		if p := o.Offers.Promo(); p != nil {
			return p
		}
	}
	return &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, TrialDays: 7}
}

func (o sampleOffers) FasterOnSale(r float64) bool {
	return (o.Offers != nil && o.Offers.FasterOnSale(r)) || r < 50
}

// present localizes a status and, for the resource page, builds its view
// from the viewer's reading as it is at now -- and last, their last reading
// on the chain while they read gone (statusview.Meter.Last), for the view the
// page's own player keeps (View.Playing). The loop calls it before
// comparing with the last message, so a second that would look the same
// sends nothing -- and, since it ticks every second, the one second the
// swarm's hold runs out does send the badge.
//
// The Vault dashboard's rows get the view's badge alone (Badge), built from
// the same status with no viewer: they draw no viewer, and the reading is
// never followed for them. The swarm's hold is theirs too, so a row does not
// blink "waiting for missing pieces" in the gaps between a transfer's pieces.
func (e *viewEnv) present(st *TorrentStatus, viewer, last statusview.Viewer, sizeBytes int64, bitrateMbps float64, now time.Time) {
	localizeStatus(e.loc, st)
	vt := st.viewTorrent(false)
	moving := 0.0
	if statusview.SwarmMoving(vt) {
		moving = vt.RateBps
	}
	held := e.hold.Swarm(moving, now)
	if !e.withView {
		b := e.build(st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, false, held).Badge
		st.Badge = &b
		return
	}
	st.View = e.build(st, viewer, last, sizeBytes, bitrateMbps, false, held)
}

// viewer is the viewer's reading the view draws at now: the session
// stream's, and through a stream to thp lost and being reopened the last one
// that had the viewer on the chain (statusview.Hold.Viewer) -- a pod
// rotation or an ingress reload is a gap in the readings, not a viewer who
// left.
func (e *viewEnv) viewer(w *sessionWatch, now time.Time) statusview.Viewer {
	return e.hold.Viewer(w.reading(now), w.reconnecting(), now)
}

// last is the viewer's last reading on the chain while the session stream
// says they left (statusview.Meter.Last): HLS closes its request between two
// segments, and the page keeps them on the chain while its own player plays.
func (e *viewEnv) last(w *sessionWatch, now time.Time) statusview.Viewer {
	return w.meter.Last(now)
}

// initialView is the view the page renders before its status stream opens:
// the Vault database's word only (prepareInitialStatus), an idle status read
// as "not asked yet", the viewer's link not drawn. No reading, so no plan
// box: the size the ETA would price does not matter yet.
func (s *Handler) initialView(c *gin.Context, st *TorrentStatus) *statusview.View {
	if st == nil {
		return nil
	}
	return s.viewEnv(c, api.GetClaimsFromContext(c)).build(st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, true, 0)
}

// build is the view of st. last: the viewer's last reading on the chain
// (statusview.Input.LastViewer); heldBps: the swarm's rate as the hold keeps
// it (statusview.Input.HeldBps).
func (e *viewEnv) build(st *TorrentStatus, viewer, last statusview.Viewer, sizeBytes int64, bitrateMbps float64, pending bool, heldBps float64) *statusview.View {
	offers := e.offers
	if e.debug {
		offers = sampleOffers{offers}
	}
	return statusview.Build(statusview.Input{
		Lang:         e.lang,
		Loc:          e.loc,
		Torrent:      st.viewTorrent(pending),
		Viewer:       viewer,
		LastViewer:   last,
		Tier:         e.tier,
		SignedIn:     e.signedIn,
		ClaimCapMbps: e.claimCap,
		Offers:       offers,
		SizeBytes:    sizeBytes,
		BitrateMbps:  bitrateMbps,
		HeldBps:      heldBps,
	})
}

// viewTorrent is the torrent side of a status for statusview. pending: the
// page is rendering and no stats were asked for yet. A still swarm has no
// rate here (swarmStill), whatever the smoothed one still says.
func (t *TorrentStatus) viewTorrent(pending bool) statusview.Torrent {
	rate := t.Rate
	if t.swarmStill {
		rate = 0
	}
	return statusview.Torrent{
		State:             t.State,
		Pending:           pending,
		Progress:          t.Progress,
		Seeders:           t.Seeders,
		Leechers:          t.Leechers,
		Peers:             t.Peers,
		SwarmKnown:        t.swarmKnown,
		RateBps:           rate,
		Checking:          t.Checking,
		Paused:            t.Paused,
		NoSeeders:         t.NoSeeders,
		Pieces:            t.Pieces != "",
		AvailabilityKnown: t.availKnown,
		Availability:      t.availability,
		Missing:           t.holes,
		WantedMissing:     t.wantedMissing,
		ReaderMissing:     t.readerMissing,
		Settling:          t.settling,
		CacheProgress:     t.cachePct,
	}
}

// rateLabelFloor: the swarm rate gets a label from 1 KB/s.
const rateLabelFloor = 1024

// localizeStatus fills the translated labels the Vault dashboard's rows and
// the page's older readers use.
func localizeStatus(loc *goi18n.Localizer, status *TorrentStatus) {
	status.Label = i18n.TranslateWithLocalizer(loc, "resource.status."+status.State)
	if status.Paused {
		status.Label = i18n.TranslateWithLocalizer(loc, "resource.status.cachingPaused")
		status.PausedHint = i18n.TranslateWithLocalizer(loc, "resource.status.cachingPausedHint")
	}
	if status.NoSeeders {
		status.Label = i18n.TranslateWithLocalizer(loc, "resource.status.noSeeders")
		status.NoSeedersHint = i18n.TranslateWithLocalizer(loc, "resource.status.noSeedersHint")
	}
	if status.Checking {
		status.Label = i18n.TranslateWithLocalizer(loc, "resource.status.checking")
	}
	status.Swarm = swarmLabel(loc, status)
	if status.Checking {
		// No claims while checking: no swarm suffix either.
		status.Swarm = ""
	}
	if status.PiecesTotal > 0 {
		status.PiecesLabel = i18n.TranslateWithLocalizerPlural(loc, "resource.status.pieces", status.PiecesTotal, map[string]any{"Done": status.PiecesDone, "Total": status.PiecesTotal})
	}
	if status.Rate >= rateLabelFloor {
		status.RateLabel = i18n.TranslateWithLocalizerData(loc, "resource.status.rate", map[string]any{"Speed": helpers.Bytes(uint64(status.Rate))})
	}
}

// swarmLabel is the badge suffix: seeders and leechers when the seeder splits
// them, the combined peer count otherwise, nothing when nothing is known.
// Terminal states carry no swarm — a cached or vaulted torrent plays
// regardless of who is around.
func swarmLabel(loc *goi18n.Localizer, st *TorrentStatus) string {
	if st.State == "cached" || st.State == "vaulted" || st.State == "unknown" || st.State == "vault_waiting" {
		return ""
	}
	switch {
	case st.Seeders > 0 || st.Leechers > 0:
		// Each count declined on its own: "2 сида · 5 личей".
		return i18n.TranslateWithLocalizerPlural(loc, "resource.status.seeders", st.Seeders, nil) + " · " +
			i18n.TranslateWithLocalizerPlural(loc, "resource.status.leechers", st.Leechers, nil)
	case st.Peers > 0:
		return i18n.TranslateWithLocalizerPlural(loc, "resource.status.peers", st.Peers, nil)
	}
	return ""
}

// debugStatus is the dev-only override for the status: with
// ?debug_status=<state> (plus optional seeders, leechers, peers, progress) the
// SSE stream emits exactly that status once instead of consulting the seeder
// and Vault, so every variant can be reviewed on any resource page. The
// viewer's own link: user_rate (bytes/s) makes it flow, viewer=zero reads
// nothing flowing (not drawn: nothing goes to them), viewer_stalled=1
// stalled, plan_limited=1 at the plan's cap (plan_rate, a rate claim,
// default 5M); without any of them it is unknown and not drawn. bitrate
// (Mbps) is what the stalled player's file needs. The swarm's availability:
// availability=0.73 says the seeder knows it (that share of the file is
// there), debug_missing=holes paints the pieces nobody has (hatched), and
// wanted_missing / reader_missing count the wanted ones and the ones an open
// reader waits on. The JS client forwards these params from the page URL.
// Inert under GIN_MODE=release, like the other debug switches.
func debugStatus(c *gin.Context, env *viewEnv) *TorrentStatus {
	if gin.Mode() == gin.ReleaseMode {
		return nil
	}
	state := c.Query("debug_status")
	switch state {
	case "idle", "caching", "cached", "vaulting", "vaulted", "unknown", "vault_failed", "vault_waiting":
	default:
		return nil
	}
	n := func(k string) int { v, _ := strconv.Atoi(c.Query(k)); return v }
	f := func(k string) float64 { v, _ := strconv.ParseFloat(c.Query(k), 64); return v }
	st := &TorrentStatus{State: state, Progress: f("progress"), Seeders: n("seeders"), Leechers: n("leechers"), Peers: n("peers"), Rate: f("rate"), Paused: state == "caching" && c.Query("paused") == "1", NoSeeders: state == "caching" && c.Query("noseeders") == "1", Checking: state == "caching" && c.Query("checking") == "1"}
	st.swarmKnown = c.Query("seeders") != "" || c.Query("peers") != "" || st.NoSeeders
	if c.Query("availability") != "" {
		st.availKnown, st.availability = true, f("availability")
		st.wantedMissing, st.readerMissing = n("wanted_missing"), n("reader_missing")
		if holes := debugHoles(c.Query("debug_missing")); holes != nil {
			st.holes, st.Missing = true, base64.StdEncoding.EncodeToString(holes)
		}
	}
	if fill, active, total := debugPieces(c.Query("debug_pieces")); fill != nil {
		st.Pieces = base64.StdEncoding.EncodeToString(fill)
		st.Active = base64.StdEncoding.EncodeToString(active)
		st.PiecesTotal = total
		for _, f := range fill {
			if f == 255 {
				st.PiecesDone += total / len(fill)
			}
		}
	}
	st.withBarPolicy()
	env.debug = true
	env.present(st, debugViewer(c), statusview.Viewer{}, debugSizeBytes, f("bitrate"), time.Now())
	return st
}

// debugSizeBytes is the preview's file, the design's "1.2 GB".
const debugSizeBytes = 1288490189

// debugViewer is the viewer's reading debugStatus asks for: on the chain
// (a request open) flowing, at the cap (plan_limited=1 long enough for the
// plan box, plan_limited=fact its first seconds) or waiting; viewer=zero --
// known, no request of theirs open.
func debugViewer(c *gin.Context) statusview.Viewer {
	capMbps := statusview.RateMbps(c.DefaultQuery("plan_rate", "5M"))
	ur, _ := strconv.ParseFloat(c.Query("user_rate"), 64)
	switch pl := c.Query("plan_limited"); {
	case pl == "1" || pl == "fact":
		return statusview.Viewer{Known: true, Present: true, Mbps: statusview.Quantize(capMbps), Limited: true, PlanBox: pl == "1", CapMbps: capMbps}
	case c.Query("viewer_stalled") == "1":
		return statusview.Viewer{Known: true, Present: true, Stalled: true, CapMbps: capMbps}
	case ur > 0:
		return statusview.Viewer{Known: true, Present: true, Mbps: statusview.Quantize(statusview.BytesToMbps(ur)), CapMbps: capMbps}
	case c.Query("viewer") == "zero":
		return statusview.Viewer{Known: true, CapMbps: capMbps}
	}
	return statusview.Viewer{}
}

// debugHoles paints the pieces nobody connected has for the dev override:
// holes -- the design's two runs, 47-61% and 75-88% of the bar.
func debugHoles(pattern string) []byte {
	if pattern != "holes" {
		return nil
	}
	bits := make([]byte, PieceBuckets/8)
	for _, r := range [][2]float64{{0.47, 0.61}, {0.75, 0.88}} {
		for c := int(r[0] * PieceBuckets); c < int(r[1]*PieceBuckets); c++ {
			bits[c/8] |= 1 << uint(c%8)
		}
	}
	return bits
}

// debugPieces paints synthetic piece bars for the dev override:
//
//	stream — head and tail complete, a fetching window a third of the way in
//	sparse — every fifth cell complete, the rest missing
//	half   — first half complete, next cell fetching
//	full   — everything complete
//	empty  — nothing yet
func debugPieces(pattern string) (fill, active []byte, total int) {
	if pattern == "" {
		return nil, nil, 0
	}
	fill = make([]byte, PieceBuckets)
	active = make([]byte, PieceBuckets/8)
	mark := func(i int) { active[i/8] |= 1 << uint(i%8) }
	switch pattern {
	case "stream":
		for i := 0; i < 24; i++ {
			fill[i] = 255
		}
		for i := PieceBuckets - 8; i < PieceBuckets; i++ {
			fill[i] = 255
		}
		for i := 80; i < 96; i++ {
			fill[i] = byte(255 - (i-80)*16)
		}
		for i := 96; i < 104; i++ {
			mark(i)
		}
	case "sparse":
		for i := 0; i < PieceBuckets; i += 5 {
			fill[i] = 255
		}
	case "half":
		for i := 0; i < PieceBuckets/2; i++ {
			fill[i] = 255
		}
		mark(PieceBuckets / 2)
		mark(PieceBuckets/2 + 1)
	case "full":
		for i := range fill {
			fill[i] = 255
		}
	case "empty":
	default:
		return nil, nil, 0
	}
	return fill, active, PieceBuckets * 4
}

// statusVault is what the status reads from Vault (*vault.Vault): the
// resource's row in the database, and the Vault API's transfer for a funded
// one.
type statusVault interface {
	GetResource(ctx context.Context, resourceID string) (*vaultModels.Resource, error)
	GetVaultAPIResource(ctx context.Context, resourceID string) (*vault.Resource, error)
}

// The loop asks Vault every vaultPollEvery ticks (a second each), and every
// vaultedPollEvery once the torrent is vaulted.
const (
	vaultPollEvery   = 2
	vaultedPollEvery = 30
)

// statusLoop runs in a background goroutine, computing status updates and sending them to the channel.
// For the resource page (env.withView) it also follows the viewer's own thp
// session stream and puts the view on every status.
func (s *Handler) statusLoop(ctx context.Context, claims *api.Claims, resourceID string, out chan<- *TorrentStatus, env *viewEnv) {
	defer close(out)

	var statsCh <-chan api.EventData
	var lastStats *TorrentStatsData
	var pieces pieceMap
	rate := ratemeter.New(0.4)
	var lastCompleted int
	// lastProgressAt is zero until Completed first grows on this stream;
	// firstStatsAt starts the observation window.
	var lastProgressAt, firstStatsAt time.Time
	// statsStale: the stream closed and a reconnect is pending. The last
	// known status keeps being shown (a frozen 51% beats a false "idle"),
	// without speed and without the paused/no-seeders verdicts — we do not
	// know. Seeder pods are rotated on every deploy, which closes every
	// stream they held; the download continues on the new pod.
	var statsStale bool
	reconnects := 0
	// statsUnavailable: the stats connection failed for a reason other than
	// "cached". Rendering that as idle made an upstream 429 or 5xx look like
	// a dead torrent; "unknown" says what we actually know — nothing.
	var statsUnavailable bool
	var lastJSON string
	var lastDBResource *vaultModels.Resource
	var lastAPIResource *vault.Resource

	statsChResult := make(chan statsConn, 1)

	// The viewer's own link (thp /session-stats), opened once the export
	// response says which node serves them — cached content included — and
	// the first status is out. Each open mints its own token.
	sess := newSessionWatch(resourceID, s.api.SessionStats, realAfter, func() (sessionToken, error) {
		return sessionStatsToken(s.api.SignClaims, claims, resourceID, time.Now())
	})
	defer sess.stop()
	// sizeBytes prices the download ETA: the page's file, else the torrent.
	var sizeBytes int64

	// Start stats connection attempt (single attempt, no retry to avoid starting idle seeders)
	go func() {
		statsChResult <- s.tryConnectStats(ctx, claims, resourceID, env.file)
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	vaultTick := 0

	scheduleReconnect := func() {
		if !shouldReconnect(lastStats, reconnects, sinceProgress(lastProgressAt)) {
			return
		}
		reconnects++
		delay := time.Duration(1<<uint(reconnects)) * time.Second // 2, 4, 8, 16, 32 s
		log.WithField("resourceID", resourceID).WithField("attempt", reconnects).WithField("in", delay).Info("status: stats stream closed mid-download, reconnecting")
		time.AfterFunc(delay, func() {
			res := s.tryConnectStats(ctx, claims, resourceID, env.file)
			select {
			case statsChResult <- res:
			case <-ctx.Done():
			}
		})
	}

	sendStatus := func() bool {
		status := resolveStatus(lastDBResource, lastAPIResource, lastStats)
		if status.State == "idle" && statsUnavailable {
			status.State = "unknown"
			status.withBarPolicy()
		}
		if lastStats != nil && !statsStale && !firstStatsAt.IsZero() {
			activity := hasActive(lastStats.Active) || (!lastProgressAt.IsZero() && time.Since(lastProgressAt) < settleAfter)
			switch judgeSwarm(status.State, time.Since(firstStatsAt), activity, lastStats.Live, lastStats.Seeders, lastStats.Peers) {
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
		if statsStale || status.Paused || status.NoSeeders || status.Checking || status.Rate < 512 {
			status.Rate = 0
		}
		now := time.Now()
		// The chain's swarm moves while its bytes arrive, not while the
		// smoothed rate is still decaying from them (movingFor).
		status.swarmStill = lastProgressAt.IsZero() || now.Sub(lastProgressAt) >= movingFor
		// The first piece can arrive before the rate meter has an interval
		// to measure it over: settled once the swarm moved, or the window
		// is over.
		status.settling = !firstStatsAt.IsZero() && now.Sub(firstStatsAt) < settleAfter && !env.hold.Moved()
		env.present(status, env.viewer(sess, now), env.last(sess, now), sizeBytes, 0, now)
		// A vaulted torrent's page stream stays open for the viewer's own
		// link; once that link cannot come, nothing is left to say.
		if env.withView && status.State == "vaulted" && sess.dead() {
			status.Final = true
		}
		data, _ := json.Marshal(status)
		jsonStr := string(data)
		if jsonStr == lastJSON {
			return true
		}
		lastJSON = jsonStr
		select {
		case out <- status:
		case <-ctx.Done():
			return false
		}
		return !endsStream(status, env)
	}

	// Fetch vault state before first send to avoid idle→vaulted flicker
	if s.statusVault != nil {
		var err error
		lastDBResource, err = s.statusVault.GetResource(ctx, resourceID)
		if err != nil {
			log.WithError(err).Warn("failed to get vault resource for initial status")
		}
		if lastDBResource != nil && lastDBResource.Funded && !lastDBResource.Vaulted {
			lastAPIResource, err = s.statusVault.GetVaultAPIResource(ctx, resourceID)
			if err != nil {
				log.WithError(err).Warn("failed to get vault api resource for initial status")
			}
		}
	}

	// Wait for first stats connection attempt before sending initial status
	// to avoid idle→caching flicker
	initialSent := false

	for {
		select {
		case <-ctx.Done():
			return

		case res := <-statsChResult:
			statsCh = res.ch
			if res.size > 0 {
				sizeBytes = res.size
			}
			log.WithField("resourceID", resourceID).WithField("connected", res.ch != nil).WithField("msg", res.msg).Info("status: stats connection result")
			// If export says content is cached (no torrent_client_stat), mark as cached
			if res.ch != nil {
				statsStale = false
			}
			if res.msg == "cached" {
				lastStats = &TorrentStatsData{Total: 1, Completed: 1, Seeders: 0}
			} else if res.ch == nil {
				if statsStale && shouldReconnect(lastStats, reconnects, sinceProgress(lastProgressAt)) {
					scheduleReconnect()
				} else {
					statsUnavailable = true
					if statsStale {
						// Retries exhausted: stop pretending to know.
						lastStats = nil
						statsStale = false
					}
				}
			}
			if !initialSent {
				if !sendStatus() {
					return
				}
				initialSent = true
			}
			// After the first status: for the Vault dashboard a torrent
			// vaulted at load has ended the stream just above, before
			// asking thp for anything.
			if env.withView {
				sess.start(ctx, res.session)
			}

		case r := <-sess.results:
			sess.opened(ctx, r)

		case ev, ok := <-sess.ch:
			sess.event(ctx, ev, ok, time.Now())
			if !sendStatus() {
				return
			}

		case ev, ok := <-statsCh:
			if ok && ev.Status == api.StatTerminated {
				// The seeder pod is going away and says so with every
				// counter zero; the stream ends right after. Read as stats,
				// it was a torrent with nothing stored: "idle", and the close
				// then reconnected to nothing (shouldReconnect wants
				// something stored) -- a deploy mid-download read "idle"
				// for good. Skipped, the close goes through shouldReconnect
				// with the real progress, and the new pod picks the
				// download up.
				log.WithField("resourceID", resourceID).Debug("status: seeder terminating")
				continue
			}
			if ok {
				pieces.apply(ev)
				fill, active := pieces.buckets()
				// Completed is verified bytes; its delta per second is the
				// swarm's useful throughput — the download speed a torrent
				// client would show.
				now := time.Now()
				rps := rate.Sample(int64(ev.Completed), now)
				if firstStatsAt.IsZero() {
					firstStatsAt = now
					lastCompleted = ev.Completed
				} else if ev.Completed != lastCompleted {
					lastCompleted = ev.Completed
					lastProgressAt = now
				}
				lastStats = &TorrentStatsData{
					Live:              ev.Live == nil || *ev.Live,
					Rate:              rps,
					Total:             ev.Total,
					Completed:         ev.Completed,
					Seeders:           ev.Seeders,
					Leechers:          ev.Leechers,
					Peers:             ev.Peers,
					Fill:              fill,
					Active:            active,
					PiecesDone:        pieces.done(),
					PiecesTotal:       len(pieces.complete),
					Holes:             pieces.holes(),
					AvailabilityKnown: ev.AvailabilityKnown,
					Availability:      ev.Availability,
					WantedMissing:     ev.WantedMissing,
					ReaderMissing:     ev.ReaderMissing,
				}
				log.WithField("resourceID", resourceID).WithField("completed", ev.Completed).WithField("total", ev.Total).WithField("peers", ev.Peers).WithField("seeders", ev.Seeders).WithField("leechers", ev.Leechers).Debug("status: got stats event")
			} else {
				// Stats channel closed — seeder gone or connection dropped.
				// Keep the last status on screen while a reconnect is due;
				// forget it only when there is nothing worth reconnecting for.
				log.WithField("resourceID", resourceID).Warn("status: stats channel closed")
				statsCh = nil
				switch {
				case shouldReconnect(lastStats, reconnects, sinceProgress(lastProgressAt)):
					statsStale = true
					scheduleReconnect()
				case lastStats != nil && lastStats.whole():
					// The seeder closes the stream once the torrent is
					// complete: nothing is left to reconnect for, and nothing
					// to forget either -- the torrent is in the cache.
					// Forgotten, it read "idle" ("Webtor ожидает" on 5461f58a…,
					// 2026-09-25). Nothing moves on it any more.
					lastStats.Rate, lastStats.Active = 0, nil
				default:
					lastStats = nil
				}
			}
			if !sendStatus() {
				return
			}

		case <-ticker.C:

			// Vaulted is final on this page (the stream stays open only for
			// the viewer's link): the database is asked far less often.
			every := vaultPollEvery
			if lastDBResource != nil && lastDBResource.Vaulted {
				every = vaultedPollEvery
			}
			if s.statusVault != nil && vaultTick%every == 0 {
				dbRes, err := s.statusVault.GetResource(ctx, resourceID)
				if err != nil {
					log.WithError(err).Warn("failed to get vault resource for status")
				} else {
					lastDBResource = dbRes
				}
				lastAPIResource = nil
				if lastDBResource != nil && lastDBResource.Funded && !lastDBResource.Vaulted {
					apiRes, err := s.statusVault.GetVaultAPIResource(ctx, resourceID)
					if err != nil {
						log.WithError(err).Warn("failed to get vault api resource for status")
					} else {
						lastAPIResource = apiRes
					}
				}
			}
			vaultTick++

			// The seeder only sends events when something changed, so a
			// swarm that stopped sends nothing — re-sample the meter with the
			// unchanged counter so the speed decays instead of freezing at
			// the last value it had when the bytes stopped.
			if lastStats != nil && statsCh != nil {
				lastStats.Rate = rate.Sample(int64(lastCompleted), time.Now())
			}

			if !sendStatus() {
				return
			}
		}
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
	log.WithField("resourceID", resourceID).WithField("url", statItem.URL[:min(len(statItem.URL), 80)]).Info("status: connecting to stats SSE")

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
