package release_subscription

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/stremio"
)

// The poller is the half of the feature that runs without a user in front
// of it: once a subscription exists, this is what asks the user's own
// sources whether anything new has turned up, and mails them when it has.
//
// Everything it touches is behind an interface, for the reason vault.go's
// reaper is: the interesting logic here is scheduling and batching, and
// neither should need a database, an SMTP server or a live indexer to be
// tested.

type pollStore interface {
	ListDue(ctx context.Context, now time.Time, limit int) ([]models.ReleaseSubscription, error)
	InsertHits(ctx context.Context, hits []models.ReleaseSubscriptionHit, baseline bool) (int, error)
	ListPendingHits(ctx context.Context, subscriptionID uuid.UUID) ([]models.ReleaseSubscriptionHit, error)
	MarkHitsNotified(ctx context.Context, subscriptionID uuid.UUID, infohashes []string) error
	MarkChecked(ctx context.Context, id uuid.UUID, state string, nextCheckAt time.Time) error
	MarkNotified(ctx context.Context, id uuid.UUID) error
	SeasonEpisodes(ctx context.Context, videoID string, season int16) ([]models.EpisodeMetadata, error)
	AccountLang(ctx context.Context, userID uuid.UUID) string
}

// streamSearch is the user's own stream pipeline — addons and indexers,
// deduplicated by infohash. Built per user because the sources are theirs.
type streamSearch interface {
	Search(ctx context.Context, u *auth.User, contentType, contentID string) ([]stremio.StreamItem, error)
}

type pollMailer interface {
	SendSubscriptionUpdate(to string, sub notification.SubscriptionView, releases []notification.ReleaseView) error
	SendSubscriptionOff(to string, sub notification.SubscriptionView, completed bool) error
}

// tierResolver answers "is this account on the free plan", which decides
// how often it is polled.
type tierResolver interface {
	IsFree(email string, patreonUserID *string) bool
}

// AiringChecker is the same capability the resource-page banner uses: does
// this series still produce episodes. Only a "no" ends a subscription — and
// because that "no" is terminal (a completed row is never polled again), the
// poller uses the checked variant: an answer that is really "could not ask"
// must not close anything. Exported so the poll command can wire nil when
// the enricher has no mappers to answer with.
type AiringChecker interface {
	IsAiringSeriesChecked(ctx context.Context, videoID string) (bool, error)
}

// PollConfig is the scheduling policy, all of it flag-backed.
type PollConfig struct {
	Batch       int
	Concurrency int
	// MaxEpisodes bounds how many episodes of a season one poll queries.
	// Releases follow airing, so the newest aired episodes are where new
	// torrents appear; season packs come back from any of those queries
	// anyway, because the season filter reads packs as covering it.
	MaxEpisodes int
	// NotifyInterval is the shortest gap between two update letters for one
	// subscription. It is what turns four new rips into one email.
	NotifyInterval time.Duration
	// IntervalHot applies while an episode aired (or is due) within
	// HotWindow; IntervalPaid and IntervalFree are the resting cadences.
	IntervalHot  time.Duration
	IntervalPaid time.Duration
	IntervalFree time.Duration
	IntervalMax  time.Duration
	HotWindow    time.Duration

	Domain string
	Secret string
}

type Poller struct {
	store  pollStore
	search streamSearch
	mail   pollMailer
	tiers  tierResolver
	airing AiringChecker
	cfg    PollConfig
	// rnd spreads next_check_at so a batch that came due together does not
	// come due together again. Seeded per poller; no cryptographic use.
	rnd   *rand.Rand
	rndMu sync.Mutex
}

func NewPoller(store pollStore, search streamSearch, mail pollMailer, tiers tierResolver, airing AiringChecker, cfg PollConfig) *Poller {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	return &Poller{
		store:  store,
		search: search,
		mail:   mail,
		tiers:  tiers,
		airing: airing,
		cfg:    cfg,
		rnd:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Run polls one batch of due subscriptions and returns how many it handled.
//
// Work is grouped by account and the groups run in parallel: one account's
// subscriptions hit the same addons and the same indexers, and running them
// side by side is how you get a private tracker to rate-limit you.
func (p *Poller) Run(ctx context.Context) (int, error) {
	subs, err := p.store.ListDue(ctx, time.Now(), p.cfg.Batch)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	byUser := map[uuid.UUID][]models.ReleaseSubscription{}
	order := make([]uuid.UUID, 0, len(subs))
	for _, sub := range subs {
		if _, ok := byUser[sub.UserID]; !ok {
			order = append(order, sub.UserID)
		}
		byUser[sub.UserID] = append(byUser[sub.UserID], sub)
	}

	log.WithField("subscriptions", len(subs)).
		WithField("accounts", len(order)).
		Info("polling release subscriptions")

	sem := make(chan struct{}, p.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, userID := range order {
		wg.Add(1)
		go func(list []models.ReleaseSubscription) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for i := range list {
				if ctx.Err() != nil {
					return
				}
				if err := p.pollOne(ctx, &list[i]); err != nil {
					log.WithError(err).
						WithField("subscription_id", list[i].ID).
						Error("failed to poll release subscription")
				}
			}
		}(byUser[userID])
	}
	wg.Wait()

	return len(subs), nil
}

// pollOne is one subscription's turn: search, record, maybe mail, reschedule.
//
// The order matters. Hits are recorded before anything is sent, and marked
// as delivered only after a letter actually goes out, so a failed send
// leaves them pending for the next run rather than losing them.
func (p *Poller) pollOne(ctx context.Context, sub *models.ReleaseSubscription) error {
	if sub.User == nil || sub.User.Email == "" {
		// Nothing to mail. Push the row far out rather than leaving it due,
		// or it comes back every run.
		return p.store.MarkChecked(ctx, sub.ID, sub.State, p.retryAt())
	}

	u := &auth.User{
		ID:            sub.UserID,
		Email:         sub.User.Email,
		PatreonUserID: sub.User.PatreonUserID,
	}

	episodes, err := p.seasonEpisodes(ctx, sub)
	if err != nil {
		return p.rescheduleAfter(ctx, sub, err)
	}

	hits, searched, err := p.collect(ctx, u, sub, episodes)
	if err != nil {
		return p.rescheduleAfter(ctx, sub, err)
	}

	baseline := sub.State == models.ReleaseSubscriptionStatePendingBaseline
	if len(hits) > 0 {
		if _, err := p.store.InsertHits(ctx, hits, baseline); err != nil {
			return p.rescheduleAfter(ctx, sub, err)
		}
	}

	state := sub.State
	notified := false
	if baseline {
		// The first look is a snapshot, not news: everything found now is
		// what the user could already see when they subscribed. But only a
		// look that actually reached a source counts as one — promoting
		// after a run where every query failed would make the next poll
		// treat the entire back catalogue as new.
		if searched {
			state = models.ReleaseSubscriptionStateActive
		}
	} else {
		sent, err := p.notify(ctx, sub, false)
		notified = sent
		if err != nil {
			// A failed letter must not stop the schedule from moving on, and
			// must not mark anything delivered. Both hold: hits stay pending
			// and we fall through to MarkChecked.
			log.WithError(err).
				WithField("subscription_id", sub.ID).
				Error("failed to send subscription update")
		}
	}

	if p.seasonIsOver(ctx, sub, episodes) {
		// Last call. Whatever is still pending goes out now, interval or
		// not: a completed row is never polled again, so anything left
		// behind here is never mentioned to anyone.
		if _, err := p.notify(ctx, sub, true); err != nil {
			log.WithError(err).
				WithField("subscription_id", sub.ID).
				Error("failed to send the final subscription update")
		}
		p.announceCompletion(ctx, sub)
		return p.store.MarkChecked(ctx, sub.ID, models.ReleaseSubscriptionStateCompleted, p.retryAt())
	}

	return p.store.MarkChecked(ctx, sub.ID, state, p.nextCheckAt(sub, u, episodes, notified))
}

// rescheduleAfter moves a failed poll to the back of the queue before
// returning its error.
//
// Without it a row that fails deterministically — one addon answering with
// an infohash the column cannot hold, say — keeps its past-due
// next_check_at, and ListDue orders by that ascending. It would sit at the
// head of every batch forever, re-running its whole outbound search each
// run and never getting further.
func (p *Poller) rescheduleAfter(ctx context.Context, sub *models.ReleaseSubscription, cause error) error {
	if err := p.store.MarkChecked(ctx, sub.ID, sub.State, p.retryAt()); err != nil {
		log.WithError(err).
			WithField("subscription_id", sub.ID).
			Error("failed to reschedule a subscription after an error")
	}
	return cause
}

// retryAt is the far-out due time used when a poll could not do its job.
// The ceiling is a flag and may be set to zero; falling back keeps a
// misconfiguration from turning into a hot loop.
func (p *Poller) retryAt() time.Time {
	d := p.cfg.IntervalMax
	if d <= 0 {
		d = 24 * time.Hour
	}
	return p.jitter(time.Now().Add(d))
}

// seasonEpisodes loads the season's episode metadata. Movies have none, and
// a season the mappers know nothing about yields an empty slice — both are
// handled by the callers rather than treated as failures.
func (p *Poller) seasonEpisodes(ctx context.Context, sub *models.ReleaseSubscription) ([]models.EpisodeMetadata, error) {
	if !sub.IsSeason() || sub.Season == nil {
		return nil, nil
	}
	eps, err := p.store.SeasonEpisodes(ctx, sub.VideoID, *sub.Season)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load season episodes")
	}
	return eps, nil
}

// collect runs the searches for one subscription and turns the results into
// hit rows.
// The bool reports whether every query actually reached the user's sources.
// "Found nothing" and "could not ask" look identical in the returned slice,
// and the baseline decision turns on the difference — per query, not per
// run: a five-episode pass where one episode's search failed has not seen
// that episode's releases, and promoting on it would mail them later as new.
func (p *Poller) collect(ctx context.Context, u *auth.User, sub *models.ReleaseSubscription, episodes []models.EpisodeMetadata) ([]models.ReleaseSubscriptionHit, bool, error) {
	var out []models.ReleaseSubscriptionHit
	seen := map[string]bool{}
	answered := 0

	queries := p.queries(sub, episodes)
	for _, q := range queries {
		items, err := p.search.Search(ctx, u, q.contentType, q.contentID)
		if err != nil {
			// One dead source must not cost the whole poll: the composite
			// stream already swallows per-source failures, so an error here
			// is the pipeline itself failing. Log and try the next query.
			log.WithError(err).
				WithField("subscription_id", sub.ID).
				WithField("content_id", q.contentID).
				Warn("stream search failed")
			continue
		}
		answered++
		for _, item := range items {
			hash := strings.ToLower(strings.TrimSpace(item.InfoHash))
			if hash == "" || seen[hash] {
				continue
			}
			seen[hash] = true
			if len(hash) > 40 {
				// The column is varchar(40) — a v1 infohash. Anything longer
				// (a BitTorrent-v2 hash, mostly) would fail the whole batch
				// INSERT, taking every valid hit in it down and putting the
				// subscription into a retry loop that can never succeed. It
				// is unusable anyway: the email links are btih magnets.
				log.WithField("subscription_id", sub.ID).
					WithField("infohash", hash).
					Debug("dropping an infohash the hit table cannot hold")
				continue
			}
			if !matchesPreferences(item, sub) {
				// Outside what this subscription asked for. Recording it
				// anyway would be worse than dropping it: the hit table is
				// what "already told you about this" means, so a release
				// stored now could never be mentioned if the preferences
				// widen later.
				continue
			}
			hit := models.ReleaseSubscriptionHit{
				SubscriptionID: sub.ID,
				InfoHash:       hash,
				Season:         sub.Season,
			}
			if name := releaseName(item); name != "" {
				hit.Name = &name
			}
			if src := sourceName(item); src != "" {
				hit.SourceName = &src
			}
			if q.episode > 0 {
				ep := int16(q.episode)
				hit.Episode = &ep
			}
			out = append(out, hit)
		}
	}
	return out, answered == len(queries), nil
}

// query is one search to run.
type query struct {
	contentType string
	contentID   string
	episode     int
}

// queries decides what to ask for.
//
// A movie is one question. A season is asked through its episodes, newest
// aired first and capped: torrents appear as episodes air, and a query for
// any episode of a season also returns that season's packs, because the
// season filter reads a pack as covering every episode in it.
//
// When nothing is known about the season's air dates — the mappers have no
// metadata for it — the fallback is episode 1. It is the one query certain
// to be answerable, and it is what surfaces the packs.
func (p *Poller) queries(sub *models.ReleaseSubscription, episodes []models.EpisodeMetadata) []query {
	if !sub.IsSeason() || sub.Season == nil {
		return []query{{contentType: "movie", contentID: sub.VideoID}}
	}

	season := int(*sub.Season)
	now := time.Now()

	aired := make([]int, 0, len(episodes))
	for _, e := range episodes {
		if e.AirDate != nil && !e.AirDate.After(now) {
			aired = append(aired, int(e.Episode))
		}
	}
	// Newest first, then cut: the tail of a season is where new releases are.
	for i, j := 0, len(aired)-1; i < j; i, j = i+1, j-1 {
		aired[i], aired[j] = aired[j], aired[i]
	}
	if len(aired) == 0 {
		aired = []int{1}
	}
	if p.cfg.MaxEpisodes > 0 && len(aired) > p.cfg.MaxEpisodes {
		aired = aired[:p.cfg.MaxEpisodes]
	}

	out := make([]query, 0, len(aired))
	for _, ep := range aired {
		out = append(out, query{
			contentType: "series",
			contentID:   fmt.Sprintf("%s:%d:%d", sub.VideoID, season, ep),
			episode:     ep,
		})
	}
	return out
}

// notify sends whatever the subscription still owes the user, if enough time
// has passed since the last letter.
func (p *Poller) notify(ctx context.Context, sub *models.ReleaseSubscription, force bool) (bool, error) {
	if !force && sub.LastNotifiedAt != nil && time.Since(*sub.LastNotifiedAt) < p.cfg.NotifyInterval {
		// Not silence — accumulation. The hits stay pending and go out
		// together once the window is up. force skips the window for the
		// one send that has no "next time": a subscription about to close.
		return false, nil
	}
	pending, err := p.store.ListPendingHits(ctx, sub.ID)
	if err != nil {
		return false, err
	}
	if len(pending) == 0 {
		return false, nil
	}

	releases := make([]notification.ReleaseView, 0, len(pending))
	hashes := make([]string, 0, len(pending))
	for i := range pending {
		h := &pending[i]
		releases = append(releases, notification.ReleaseView{
			Name:     h.GetName(),
			InfoHash: h.InfoHash,
			URL:      p.magnetURL(h),
			Source:   strOr(h.SourceName, ""),
		})
		hashes = append(hashes, h.InfoHash)
	}

	if err := p.mail.SendSubscriptionUpdate(sub.User.Email, p.view(ctx, sub), releases); err != nil {
		return false, err
	}
	if err := p.store.MarkHitsNotified(ctx, sub.ID, hashes); err != nil {
		return true, err
	}
	return true, p.store.MarkNotified(ctx, sub.ID)
}

// announceCompletion tells the user their season is done. The state itself
// is written by the MarkChecked that follows — one UPDATE, not two.
func (p *Poller) announceCompletion(ctx context.Context, sub *models.ReleaseSubscription) {
	if err := p.mail.SendSubscriptionOff(sub.User.Email, p.view(ctx, sub), true); err != nil {
		log.WithError(err).
			WithField("subscription_id", sub.ID).
			Error("failed to send subscription completion notice")
	}
}

// seasonIsOver reports whether a season subscription has run out of future:
// no episode still to air, and the series no longer in production.
//
// Both halves are needed. Air dates alone would end a subscription during
// the gap between a finale and the next season's schedule being published,
// which is exactly when someone following the show wants to stay subscribed.
// A movie subscription never ends here — the user decides when they have the
// release they wanted.
func (p *Poller) seasonIsOver(ctx context.Context, sub *models.ReleaseSubscription, episodes []models.EpisodeMetadata) bool {
	if !sub.IsSeason() {
		return false
	}
	if len(episodes) == 0 {
		// Nothing known about the season. Treating that as "over" would
		// silently kill subscriptions for shows our mappers are simply
		// behind on.
		return false
	}
	now := time.Now()
	for _, e := range episodes {
		if e.AirDate == nil || e.AirDate.After(now) {
			return false
		}
	}
	if p.airing == nil {
		return false
	}
	airing, err := p.airing.IsAiringSeriesChecked(ctx, sub.VideoID)
	if err != nil {
		// "Could not ask" is not "no". Completion is terminal — a closed
		// row is never polled again — so an unanswered check keeps the
		// subscription open and the next poll asks again.
		log.WithError(err).
			WithField("subscription_id", sub.ID).
			Warn("airing check failed; keeping the subscription open")
		return false
	}
	return !airing
}

// nextCheckAt picks when to look again.
//
// Three inputs, in order of weight: whether an episode is airing right about
// now, whether the account pays, and how long it has been since this
// subscription last had anything to say.
//
// The last one is a backoff without a counter column: a subscription that
// has produced nothing for weeks is a subscription whose show is between
// seasons, and it does not need eight looks a day. It reads the timestamps
// the row already carries, so nothing has to be tracked to make it work.
func (p *Poller) nextCheckAt(sub *models.ReleaseSubscription, u *auth.User, episodes []models.EpisodeMetadata, justNotified bool) time.Time {
	now := time.Now()
	free := p.tiers == nil || p.tiers.IsFree(u.Email, u.PatreonUserID)

	interval := p.cfg.IntervalFree
	if !free {
		interval = p.cfg.IntervalPaid
		if p.hasFreshEpisode(episodes, now) {
			interval = p.cfg.IntervalHot
		}
	}

	// The row in hand was read before this run's letter went out, so its
	// LastNotifiedAt is stale by exactly the event that matters: a
	// subscription that has just delivered something is the opposite of
	// idle, and must not be backed off for the silence that preceded it.
	idleSince := sub.CreatedAt
	if justNotified {
		idleSince = now
	} else if sub.LastNotifiedAt != nil {
		idleSince = *sub.LastNotifiedAt
	}
	switch idle := now.Sub(idleSince); {
	case idle > 21*24*time.Hour:
		interval *= 4
	case idle > 7*24*time.Hour:
		interval *= 2
	}
	interval = p.jitterDuration(interval)
	// The ceiling is applied after the jitter, not before: a ±10% spread on
	// top of the cap would put the longest intervals above it, and a
	// ceiling that the schedule routinely exceeds is not a ceiling.
	if p.cfg.IntervalMax > 0 && interval > p.cfg.IntervalMax {
		interval = p.cfg.IntervalMax
	}
	return now.Add(interval)
}

// hasFreshEpisode reports whether an episode aired, or is due, inside the
// hot window — the hours around a broadcast, when releases actually appear.
func (p *Poller) hasFreshEpisode(episodes []models.EpisodeMetadata, now time.Time) bool {
	if p.cfg.HotWindow <= 0 {
		return false
	}
	for _, e := range episodes {
		if e.AirDate == nil {
			continue
		}
		if delta := e.AirDate.Sub(now); delta > -p.cfg.HotWindow && delta < p.cfg.HotWindow {
			return true
		}
	}
	return false
}

// jitterDuration spreads an interval by up to ±10%, so subscriptions created
// in the same minute — or rescheduled by the same run — stop coming due as a
// burst.
func (p *Poller) jitterDuration(d time.Duration) time.Duration {
	spread := d / 10
	if spread <= 0 {
		return d
	}
	p.rndMu.Lock()
	defer p.rndMu.Unlock()
	return d + time.Duration(p.rnd.Int63n(int64(2*spread))) - spread
}

// jitter is the same spread applied to an absolute due time.
func (p *Poller) jitter(t time.Time) time.Time {
	return time.Now().Add(p.jitterDuration(time.Until(t)))
}

func (p *Poller) view(ctx context.Context, sub *models.ReleaseSubscription) notification.SubscriptionView {
	return subscriptionView(ctx, p.store, sub, p.cfg.Domain, p.cfg.Secret)
}

// magnetURL builds the link an email row points at. Webtor accepts a magnet
// as a path, so this works whether or not the torrent has ever passed
// through our store.
func (p *Poller) magnetURL(h *models.ReleaseSubscriptionHit) string {
	magnet := "magnet:?xt=urn:btih:" + h.InfoHash
	if h.Name != nil && *h.Name != "" {
		magnet += "&dn=" + url.QueryEscape(*h.Name)
	}
	return strings.TrimRight(p.cfg.Domain, "/") + "/" + magnet
}

// matchesPreferences applies the subscription's own resolution and language
// overrides — a copy of the account's stream settings at the time it was
// created, editable in the profile since.
//
// Empty means no preference, which is why a subscription made before these
// columns existed reports everything, as it always did.
func matchesPreferences(item stremio.StreamItem, sub *models.ReleaseSubscription) bool {
	if len(sub.PreferredResolutions) > 0 && !slices.Contains(sub.PreferredResolutions, stremio.ResolutionBucket(item.Name)) {
		return false
	}
	if code := sub.GetPreferredLanguage(); code != "" {
		want := stremio.LanguageByCode(code)
		// An unknown code is not a filter — refusing everything because a
		// language cannot be resolved would silence the subscription.
		if want != nil && !stremio.StreamMatchesLanguage(&item, want) {
			return false
		}
	}
	return true
}

// releaseName reads the release name out of a stream item. Both sources put
// it on the first line of Title — the addons by convention, the indexers
// because LangFilterStream reads languages from exactly there.
func releaseName(item stremio.StreamItem) string {
	if line := firstLine(item.Title); line != "" {
		return line
	}
	return firstLine(item.Name)
}

// sourceName reads which addon or tracker answered. First line of Name, by
// the same convention.
func sourceName(item stremio.StreamItem) string {
	return firstLine(item.Name)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func strOr(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return *s
}
