package recommendations

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
)

// claudeItem is what Claude actually returns via tool use, before we turn it
// into a real Recommendation. Public to the package so claude.go and the
// resolver can share the shape without going through JSON twice.
type claudeItem struct {
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Reason string `json:"reason"`
}

// Resolver turns Claude's (title, year, reason) tuples into rendered
// Recommendation cards. It is intentionally thin:
//
//  1. For each claudeItem, call MetadataLookup.LookupByTitleYear once. In
//     production this hits TMDB → OMDB → Kinopoisk via the enricher, but the
//     resolver stays agnostic about that ordering.
//
//  2. Drop any item that either (a) didn't resolve, or (b) resolved to a
//     non-IMDB VideoID. Stremio add-ons (Cinemeta and friends) only know how
//     to fetch streams by IMDB tt-id — a tmdbXXXXX fallback would render a
//     card the user can't click through, so we'd rather show fewer, working
//     cards than more broken ones.
//
//  3. Preserve Claude's original ordering: the model ranks by relevance, not
//     by alphabet. Sorting would destroy signal.
//
// Lookups run concurrently through a fixed-size semaphore. The default is 5,
// which is comfortably below TMDB's 40 req/10s burst limit and well inside
// the underlying HTTP client's connection pool.
type Resolver struct {
	lookup      MetadataLookup
	concurrency int
}

// NewResolver wires a resolver with the given metadata provider. concurrency
// <= 0 falls back to 5.
func NewResolver(lookup MetadataLookup, concurrency int) *Resolver {
	if concurrency <= 0 {
		concurrency = 5
	}
	return &Resolver{lookup: lookup, concurrency: concurrency}
}

// Resolve turns a slice of claudeItems into Recommendation cards, preserving
// input order and silently dropping unresolvable entries. Never returns an
// error — a failure to resolve a single item is logged and treated as "not
// that title", not as a hard failure of the whole batch.
func (r *Resolver) Resolve(ctx context.Context, items []claudeItem, ct models.ContentType) []Recommendation {
	if len(items) == 0 {
		return nil
	}

	// Timing instrumentation. We track the wall clock for the whole batch
	// plus an atomic counter of in-flight goroutines so the summary log can
	// report the observed peak parallelism. If the peak ever falls to 1 we
	// know something is silently serializing the lookups (a hidden mutex,
	// a global limiter, etc.) and the configured concurrency is a lie.
	start := time.Now()
	var inflight, peakInflight int64

	out := make([]*Recommendation, len(items))
	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup

	for i := range items {
		i := i
		item := items[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			cur := atomic.AddInt64(&inflight, 1)
			for {
				peak := atomic.LoadInt64(&peakInflight)
				if cur <= peak || atomic.CompareAndSwapInt64(&peakInflight, peak, cur) {
					break
				}
			}
			defer atomic.AddInt64(&inflight, -1)

			rec := r.resolveOne(ctx, item, ct)
			if rec != nil {
				out[i] = rec
			}
		}()
	}
	wg.Wait()

	// Collapse into a dense slice preserving order.
	dense := make([]Recommendation, 0, len(items))
	for _, rec := range out {
		if rec != nil {
			dense = append(dense, *rec)
		}
	}

	log.WithFields(log.Fields{
		"feature":     "ai_rec",
		"items_in":    len(items),
		"items_out":   len(dense),
		"concurrency": r.concurrency,
		"peak_par":    peakInflight,
		"elapsed_ms":  time.Since(start).Milliseconds(),
	}).Info("resolver finished")

	return dense
}

// ResolveStream is the streaming sibling of Resolve. It pushes each
// successfully resolved Recommendation onto `out` as soon as it's ready,
// instead of waiting for the whole batch to finish. The channel is closed
// when all goroutines exit (either because they finished or because ctx
// was cancelled).
//
// Order is NOT preserved — items arrive on the channel in completion order.
// For the AI feature this is intentional: the user sees the fastest
// lookups first, which feels much snappier than waiting for the slowest
// item to drag the whole batch.
//
// Cancellation: callers that decide they have enough items can call
// context.WithCancel and trigger cancel(). The in-flight goroutines will
// abort their TMDB calls (which respect ctx) and exit. Callers MUST drain
// `out` until it closes, otherwise the goroutines block forever waiting
// to push their result.
func (r *Resolver) ResolveStream(ctx context.Context, items []claudeItem, ct models.ContentType, out chan<- Recommendation) {
	defer close(out)
	if len(items) == 0 {
		return
	}

	start := time.Now()
	var inflight, peakInflight, sent int64

	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup

	for _, item := range items {
		item := item
		wg.Add(1)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			continue
		}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			cur := atomic.AddInt64(&inflight, 1)
			for {
				peak := atomic.LoadInt64(&peakInflight)
				if cur <= peak || atomic.CompareAndSwapInt64(&peakInflight, peak, cur) {
					break
				}
			}
			defer atomic.AddInt64(&inflight, -1)

			rec := r.resolveOne(ctx, item, ct)
			if rec == nil {
				return
			}
			select {
			case out <- *rec:
				atomic.AddInt64(&sent, 1)
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()

	log.WithFields(log.Fields{
		"feature":     "ai_rec",
		"mode":        "stream",
		"items_in":    len(items),
		"items_sent":  sent,
		"concurrency": r.concurrency,
		"peak_par":    peakInflight,
		"elapsed_ms":  time.Since(start).Milliseconds(),
	}).Info("resolver finished")
}

// ResolveStreamFromChannel is the doubly-streaming variant: items arrive
// on `in` over time (instead of being known up front) and resolved
// recommendations land on `out` as soon as their TMDB lookup finishes.
//
// This is the bridge between the streaming Claude pipeline (which
// produces claudeItems token-by-token as the model generates them) and
// the SSE handler (which wants Recommendations as soon as possible). The
// resolver no longer waits for Claude to finish before kicking off
// lookups — the moment Claude emits the first complete `{title, year,
// reason}` triple, this method spins up a TMDB goroutine for it while
// Claude is still generating items 2..N.
//
// Closes `out` after `in` is drained AND every in-flight goroutine has
// finished. Cancellation: if ctx is cancelled, in-flight goroutines bail
// (their TMDB calls inherit the context), and the method drains `in` so
// upstream producers don't block forever waiting to push their next item.
func (r *Resolver) ResolveStreamFromChannel(ctx context.Context, in <-chan claudeItem, ct models.ContentType, out chan<- Recommendation) {
	defer close(out)

	start := time.Now()
	var inflight, peakInflight, sent int64

	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup

	for item := range in {
		item := item
		// Acquire semaphore slot, but bail if ctx fires while we're
		// waiting on it. We deliberately go(){drain} the input channel
		// in the cancel branch so the upstream Claude streamer can
		// finish its current write and exit cleanly.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			go func() { //nolint:wsl
				for range in {
				}
			}()
			wg.Wait()
			r.logFinished("stream-from-chan", -1, sent, peakInflight, time.Since(start))
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			cur := atomic.AddInt64(&inflight, 1)
			for {
				peak := atomic.LoadInt64(&peakInflight)
				if cur <= peak || atomic.CompareAndSwapInt64(&peakInflight, peak, cur) {
					break
				}
			}
			defer atomic.AddInt64(&inflight, -1)

			rec := r.resolveOne(ctx, item, ct)
			if rec == nil {
				return
			}
			select {
			case out <- *rec:
				atomic.AddInt64(&sent, 1)
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()

	r.logFinished("stream-from-chan", -1, sent, peakInflight, time.Since(start))
}

// logFinished is the shared timing log used by both streaming flavours of
// the resolver. items_in == -1 indicates "we don't know" (channel-fed mode).
func (r *Resolver) logFinished(mode string, itemsIn int, sent int64, peakPar int64, elapsed time.Duration) {
	fields := log.Fields{
		"feature":     "ai_rec",
		"mode":        mode,
		"items_sent":  sent,
		"concurrency": r.concurrency,
		"peak_par":    peakPar,
		"elapsed_ms":  elapsed.Milliseconds(),
	}
	if itemsIn >= 0 {
		fields["items_in"] = itemsIn
	}
	log.WithFields(fields).Info("resolver finished")
}

// resolveOne performs a single metadata lookup and shapes the result into a
// Recommendation. Returns nil if the item should be dropped.
func (r *Resolver) resolveOne(ctx context.Context, item claudeItem, ct models.ContentType) *Recommendation {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return nil
	}
	var year *int16
	if item.Year > 0 {
		y := int16(item.Year)
		year = &y
	}

	md, err := r.lookup.LookupByTitleYear(ctx, title, year, ct)
	if err != nil {
		log.WithError(err).
			WithField("feature", "ai_rec").
			WithField("title", title).
			Warn("metadata lookup failed")
		return nil
	}
	if md == nil {
		log.WithField("feature", "ai_rec").
			WithField("title", title).
			WithField("year", item.Year).
			Info("no metadata match; dropping recommendation")
		return nil
	}
	if !strings.HasPrefix(md.VideoID, "tt") {
		// Non-IMDB id — Stremio addons can't stream this, so showing the
		// card would be a dead end. Log loud enough to alert on the ratio
		// later if it becomes a real problem.
		log.WithField("feature", "ai_rec").
			WithField("title", title).
			WithField("video_id", md.VideoID).
			Warn("dropping non-IMDB recommendation")
		return nil
	}

	rating := 0.0
	if md.Rating != nil {
		rating = *md.Rating
	}
	posterTitle := md.Title
	if posterTitle == "" {
		posterTitle = title
	}
	recType := "movie"
	if ct == models.ContentTypeSeries {
		recType = "series"
	}
	// Poster URL goes through the /lib proxy (S3-cached, resized) rather
	// than exposing the raw TMDB/OMDB URL to the client. This matches the
	// pattern used by the library grid and watch-history — see
	// handlers/library/poster.go and models/watch_history.go.
	poster := fmt.Sprintf("/lib/%s/poster/%s/240.jpg", recType, md.VideoID)

	return &Recommendation{
		VideoID: md.VideoID,
		Title:   posterTitle,
		Year:    md.Year,
		Poster:  poster,
		Plot:    md.Plot,
		Rating:  rating,
		Reason:  strings.TrimSpace(item.Reason),
		Type:    recType,
	}
}
