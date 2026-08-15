package release_subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/stremio"
)

// --- fakes ---

type fakeStore struct {
	due      []models.ReleaseSubscription
	episodes []models.EpisodeMetadata
	pending  []models.ReleaseSubscriptionHit

	inserted       []models.ReleaseSubscriptionHit
	insertBaseline bool
	insertErr      error
	episodesErr    error
	notifiedHashes []string
	checkedState   string
	checkedNext    time.Time
	markedNotified bool
	lang           string
}

func (s *fakeStore) ListDue(context.Context, time.Time, int) ([]models.ReleaseSubscription, error) {
	return s.due, nil
}

func (s *fakeStore) InsertHits(_ context.Context, hits []models.ReleaseSubscriptionHit, baseline bool) (int, error) {
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	s.inserted = append(s.inserted, hits...)
	s.insertBaseline = baseline
	return len(hits), nil
}

func (s *fakeStore) ListPendingHits(context.Context, uuid.UUID) ([]models.ReleaseSubscriptionHit, error) {
	return s.pending, nil
}

func (s *fakeStore) MarkHitsNotified(_ context.Context, _ uuid.UUID, infohashes []string) error {
	s.notifiedHashes = append(s.notifiedHashes, infohashes...)
	return nil
}

func (s *fakeStore) MarkChecked(_ context.Context, _ uuid.UUID, state string, next time.Time) error {
	s.checkedState = state
	s.checkedNext = next
	return nil
}

func (s *fakeStore) MarkNotified(context.Context, uuid.UUID) error {
	s.markedNotified = true
	return nil
}

func (s *fakeStore) SeasonEpisodes(context.Context, string, int16) ([]models.EpisodeMetadata, error) {
	return s.episodes, s.episodesErr
}

func (s *fakeStore) AccountLang(context.Context, uuid.UUID) string { return s.lang }

type fakeSearch struct {
	byContentID map[string][]stremio.StreamItem
	asked       []string
	err         error
}

func (s *fakeSearch) Search(_ context.Context, _ *auth.User, _, contentID string) ([]stremio.StreamItem, error) {
	s.asked = append(s.asked, contentID)
	if s.err != nil {
		return nil, s.err
	}
	return s.byContentID[contentID], nil
}

type fakeMailer struct {
	updates  [][]notification.ReleaseView
	offs     []bool
	failWith error
}

func (m *fakeMailer) SendSubscriptionUpdate(_ string, _ notification.SubscriptionView, releases []notification.ReleaseView) error {
	if m.failWith != nil {
		return m.failWith
	}
	m.updates = append(m.updates, releases)
	return nil
}

func (m *fakeMailer) SendSubscriptionOff(_ string, _ notification.SubscriptionView, completed bool) error {
	m.offs = append(m.offs, completed)
	return nil
}

type fakeTier struct{ free bool }

func (t fakeTier) IsFree(string, *string) bool { return t.free }

type fakeAiring struct{ airing bool }

func (a fakeAiring) IsAiringSeries(context.Context, string) bool { return a.airing }

// --- helpers ---

func testConfig() PollConfig {
	return PollConfig{
		Batch:          100,
		Concurrency:    1,
		MaxEpisodes:    3,
		NotifyInterval: 12 * time.Hour,
		IntervalHot:    3 * time.Hour,
		IntervalPaid:   6 * time.Hour,
		IntervalFree:   12 * time.Hour,
		IntervalMax:    24 * time.Hour,
		HotWindow:      72 * time.Hour,
		Domain:         "https://webtor.io",
		Secret:         "test-secret",
	}
}

func seasonSub() *models.ReleaseSubscription {
	season := int16(3)
	title := "The Boys"
	return &models.ReleaseSubscription{
		ID:        uuid.NewV4(),
		UserID:    uuid.NewV4(),
		Kind:      models.ReleaseSubscriptionKindSeason,
		VideoID:   "tt1190634",
		Season:    &season,
		Title:     &title,
		Lang:      "en",
		Enabled:   true,
		State:     models.ReleaseSubscriptionStateActive,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		User:      &models.User{Email: "viewer@example.com"},
	}
}

func episode(n int16, airedAgo time.Duration) models.EpisodeMetadata {
	at := time.Now().Add(-airedAgo)
	return models.EpisodeMetadata{VideoID: "tt1190634", Season: 3, Episode: n, AirDate: &at}
}

func futureEpisode(n int16, in time.Duration) models.EpisodeMetadata {
	at := time.Now().Add(in)
	return models.EpisodeMetadata{VideoID: "tt1190634", Season: 3, Episode: n, AirDate: &at}
}

// --- tests ---

// TestQueriesSeason pins what a season poll actually asks for: the newest
// aired episodes, capped, newest first. Asking for every episode of a long
// season would multiply one subscription into a dozen outbound requests per
// source, and the old episodes are precisely where nothing new appears.
func TestQueriesSeason(t *testing.T) {
	p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{}, testConfig())
	sub := seasonSub()

	episodes := []models.EpisodeMetadata{
		episode(1, 28*24*time.Hour),
		episode(2, 21*24*time.Hour),
		episode(3, 14*24*time.Hour),
		episode(4, 7*24*time.Hour),
		futureEpisode(5, 24*time.Hour),
	}

	qs := p.queries(sub, episodes)
	if len(qs) != 3 {
		t.Fatalf("queries: got %d, want 3 (MaxEpisodes)", len(qs))
	}
	want := []string{"tt1190634:3:4", "tt1190634:3:3", "tt1190634:3:2"}
	for i, q := range qs {
		if q.contentID != want[i] {
			t.Errorf("query %d: got %q, want %q", i, q.contentID, want[i])
		}
		if q.contentType != "series" {
			t.Errorf("query %d: content type %q, want series", i, q.contentType)
		}
	}
}

// TestQueriesSeasonWithoutAirDates covers a season the mappers know nothing
// about. Asking nothing would make the subscription permanently silent; the
// first episode is the one query certain to be answerable, and it is what
// brings back the season packs.
func TestQueriesSeasonWithoutAirDates(t *testing.T) {
	p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{}, testConfig())
	qs := p.queries(seasonSub(), nil)
	if len(qs) != 1 || qs[0].contentID != "tt1190634:3:1" {
		t.Fatalf("queries: got %+v, want a single tt1190634:3:1", qs)
	}
}

func TestQueriesMovie(t *testing.T) {
	p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{}, testConfig())
	sub := &models.ReleaseSubscription{Kind: models.ReleaseSubscriptionKindMovie, VideoID: "tt0111161"}
	qs := p.queries(sub, nil)
	if len(qs) != 1 || qs[0].contentType != "movie" || qs[0].contentID != "tt0111161" {
		t.Fatalf("queries: got %+v, want a single movie query", qs)
	}
}

// TestBaselineDoesNotMail is the guard against the worst first impression
// the feature could make: subscribing to a season halfway through and being
// mailed its entire back catalogue a minute later.
func TestBaselineDoesNotMail(t *testing.T) {
	sub := seasonSub()
	sub.State = models.ReleaseSubscriptionStatePendingBaseline

	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(1, 14*24*time.Hour)},
		pending:  []models.ReleaseSubscriptionHit{{InfoHash: "aa", SubscriptionID: sub.ID}},
	}
	search := &fakeSearch{byContentID: map[string][]stremio.StreamItem{
		"tt1190634:3:1": {{InfoHash: "AA", Title: "The.Boys.S03E01\n👤 20", Name: "Torrentio\n1080p"}},
	}}
	mail := &fakeMailer{}
	p := NewPoller(store, search, mail, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !store.insertBaseline {
		t.Error("first poll must record its findings as baseline")
	}
	if len(mail.updates) != 0 {
		t.Errorf("first poll sent %d emails, want 0", len(mail.updates))
	}
	if store.checkedState != models.ReleaseSubscriptionStateActive {
		t.Errorf("state after baseline: got %q, want active", store.checkedState)
	}
	if len(store.inserted) != 1 || store.inserted[0].InfoHash != "aa" {
		t.Errorf("inserted hits: %+v, want one normalised hash", store.inserted)
	}
}

// TestActivePollMailsPending checks the ordinary run: what is pending goes
// out in one letter, and only then is it marked as delivered.
func TestActivePollMailsPending(t *testing.T) {
	sub := seasonSub()
	name := "The.Boys.S03E05.1080p"
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(5, 2*24*time.Hour)},
		pending: []models.ReleaseSubscriptionHit{
			{SubscriptionID: sub.ID, InfoHash: "aa", Name: &name},
			{SubscriptionID: sub.ID, InfoHash: "bb"},
		},
	}
	mail := &fakeMailer{}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(mail.updates) != 1 {
		t.Fatalf("emails sent: got %d, want 1", len(mail.updates))
	}
	if len(mail.updates[0]) != 2 {
		t.Errorf("releases in the email: got %d, want 2 — a batch is one letter", len(mail.updates[0]))
	}
	if got := mail.updates[0][1].Name; got != "bb" {
		t.Errorf("nameless release rendered as %q, want its infohash", got)
	}
	if len(store.notifiedHashes) != 2 || !store.markedNotified {
		t.Errorf("hits marked: %v, notified stamp: %v — both must follow a successful send", store.notifiedHashes, store.markedNotified)
	}
}

// TestFailedMailKeepsHitsPending is the reason MarkHitsNotified runs after
// the send and not before: a hash marked as delivered is a hash the user
// will never hear about.
func TestFailedMailKeepsHitsPending(t *testing.T) {
	sub := seasonSub()
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(5, 2*24*time.Hour)},
		pending:  []models.ReleaseSubscriptionHit{{SubscriptionID: sub.ID, InfoHash: "aa"}},
	}
	mail := &fakeMailer{failWith: errors.New("smtp is down")}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(store.notifiedHashes) != 0 || store.markedNotified {
		t.Error("a failed send must leave the hits pending")
	}
	if store.checkedState == "" {
		t.Error("a failed send must not stop the schedule from moving on")
	}
}

// TestNotifyIntervalBatches covers the accumulation window: a subscription
// mailed an hour ago waits rather than sending a second letter, and its
// hits stay pending so they ride along with the next one.
func TestNotifyIntervalBatches(t *testing.T) {
	sub := seasonSub()
	recent := time.Now().Add(-time.Hour)
	sub.LastNotifiedAt = &recent
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(5, 2*24*time.Hour)},
		pending:  []models.ReleaseSubscriptionHit{{SubscriptionID: sub.ID, InfoHash: "aa"}},
	}
	mail := &fakeMailer{}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.updates) != 0 {
		t.Errorf("emails sent: got %d, want 0 inside the notify interval", len(mail.updates))
	}
	if len(store.notifiedHashes) != 0 {
		t.Error("hits must stay pending while the batch accumulates")
	}
}

// TestSeasonIsOver covers both halves of the completion rule. Air dates
// alone would end a subscription during the gap between a finale and the
// next season's schedule — exactly when its owner wants to stay subscribed.
func TestSeasonIsOver(t *testing.T) {
	for _, tt := range []struct {
		name     string
		episodes []models.EpisodeMetadata
		airing   bool
		want     bool
	}{
		{
			name:     "all aired and the show has ended",
			episodes: []models.EpisodeMetadata{episode(1, 30*24*time.Hour), episode(2, 23*24*time.Hour)},
			airing:   false,
			want:     true,
		},
		{
			name:     "all aired but still in production",
			episodes: []models.EpisodeMetadata{episode(1, 30*24*time.Hour)},
			airing:   true,
			want:     false,
		},
		{
			name:     "an episode is still to come",
			episodes: []models.EpisodeMetadata{episode(1, 7*24*time.Hour), futureEpisode(2, 24*time.Hour)},
			airing:   false,
			want:     false,
		},
		{
			name:     "an episode has no air date at all",
			episodes: []models.EpisodeMetadata{episode(1, 7*24*time.Hour), {VideoID: "tt1190634", Season: 3, Episode: 2}},
			airing:   false,
			want:     false,
		},
		{
			name:     "nothing known about the season",
			episodes: nil,
			airing:   false,
			want:     false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{airing: tt.airing}, testConfig())
			if got := p.seasonIsOver(context.Background(), seasonSub(), tt.episodes); got != tt.want {
				t.Errorf("seasonIsOver: got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMovieNeverCompletes: the owner decides when they have the release they
// were waiting for. Nothing the poller can see says it for them.
func TestMovieNeverCompletes(t *testing.T) {
	p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{airing: false}, testConfig())
	sub := &models.ReleaseSubscription{Kind: models.ReleaseSubscriptionKindMovie, VideoID: "tt0111161"}
	if p.seasonIsOver(context.Background(), sub, nil) {
		t.Error("a movie subscription must never complete on its own")
	}
}

// TestNextCheckAt pins the schedule: tier decides the resting cadence, an
// airing episode overrides it for paying accounts, and a subscription that
// has been quiet for weeks backs off on its own.
func TestNextCheckAt(t *testing.T) {
	cfg := testConfig()
	longAgo := time.Now().Add(-30 * 24 * time.Hour)

	for _, tt := range []struct {
		name     string
		free     bool
		episodes []models.EpisodeMetadata
		notified *time.Time
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{
			name:     "paid account with an episode airing",
			episodes: []models.EpisodeMetadata{episode(5, 12*time.Hour)},
			wantMin:  2*time.Hour + 30*time.Minute,
			wantMax:  3*time.Hour + 30*time.Minute,
		},
		{
			name:     "paid account between episodes",
			episodes: []models.EpisodeMetadata{episode(5, 30*24*time.Hour)},
			wantMin:  5 * time.Hour,
			wantMax:  7 * time.Hour,
		},
		{
			name:     "free account, airing or not",
			free:     true,
			episodes: []models.EpisodeMetadata{episode(5, 12*time.Hour)},
			wantMin:  10 * time.Hour,
			wantMax:  14 * time.Hour,
		},
		{
			name:     "quiet for a month: backed off to the ceiling",
			episodes: []models.EpisodeMetadata{episode(5, 60*24*time.Hour)},
			notified: &longAgo,
			wantMin:  21 * time.Hour,
			wantMax:  24*time.Hour + 30*time.Minute,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPoller(&fakeStore{}, &fakeSearch{}, &fakeMailer{}, fakeTier{free: tt.free}, fakeAiring{}, cfg)
			sub := seasonSub()
			sub.LastNotifiedAt = tt.notified
			u := &auth.User{Email: "viewer@example.com"}

			got := time.Until(p.nextCheckAt(sub, u, tt.episodes, false))
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("next check in %v, want between %v and %v", got.Round(time.Minute), tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestCollectReadsNamesFromTheRightLines: both stream sources put the
// release name on the first line of Title and the source label on the first
// line of Name. An email that swapped them would name Torrentio as the
// release and the release as the source.
func TestCollectReadsNamesFromTheRightLines(t *testing.T) {
	sub := seasonSub()
	search := &fakeSearch{byContentID: map[string][]stremio.StreamItem{
		"tt1190634:3:5": {{
			InfoHash: "AbCdEf",
			Title:    "The.Boys.S03E05.1080p.WEB-DL\n👤 42 💾 3.1 GB",
			Name:     "RuTracker.org\n1080p",
		}},
	}}
	p := NewPoller(&fakeStore{}, search, &fakeMailer{}, fakeTier{}, fakeAiring{}, testConfig())

	hits, _, err := p.collect(context.Background(), &auth.User{}, sub, []models.EpisodeMetadata{episode(5, time.Hour)})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1", len(hits))
	}
	h := hits[0]
	if h.InfoHash != "abcdef" {
		t.Errorf("infohash: got %q, want it lower-cased", h.InfoHash)
	}
	if h.Name == nil || *h.Name != "The.Boys.S03E05.1080p.WEB-DL" {
		t.Errorf("release name: got %v", h.Name)
	}
	if h.SourceName == nil || *h.SourceName != "RuTracker.org" {
		t.Errorf("source name: got %v", h.SourceName)
	}
	if h.Episode == nil || *h.Episode != 5 {
		t.Errorf("episode: got %v, want 5", h.Episode)
	}
}

// TestCollectDedupesAcrossEpisodeQueries: a season pack answers the query
// for every episode in the season, so the same infohash comes back from
// each. One row, not three.
func TestCollectDedupesAcrossEpisodeQueries(t *testing.T) {
	pack := stremio.StreamItem{InfoHash: "pack", Title: "The.Boys.S03.COMPLETE", Name: "Torrentio"}
	search := &fakeSearch{byContentID: map[string][]stremio.StreamItem{
		"tt1190634:3:3": {pack},
		"tt1190634:3:2": {pack},
		"tt1190634:3:1": {pack},
	}}
	p := NewPoller(&fakeStore{}, search, &fakeMailer{}, fakeTier{}, fakeAiring{}, testConfig())

	episodes := []models.EpisodeMetadata{episode(1, 21*24*time.Hour), episode(2, 14*24*time.Hour), episode(3, 7*24*time.Hour)}
	hits, _, err := p.collect(context.Background(), &auth.User{}, seasonSub(), episodes)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1 — the pack answers every episode query", len(hits))
	}
	if len(search.asked) != 3 {
		t.Errorf("queries run: got %d, want 3", len(search.asked))
	}
}

// TestCompletionSendsNotice: when a season runs out of future the
// subscription ends, and the user is told why.
func TestCompletionSendsNotice(t *testing.T) {
	sub := seasonSub()
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(1, 60*24*time.Hour), episode(2, 53*24*time.Hour)},
	}
	mail := &fakeMailer{}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: false}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.offs) != 1 || !mail.offs[0] {
		t.Errorf("completion notice: got %v, want one marked as completed", mail.offs)
	}
	if store.checkedState != models.ReleaseSubscriptionStateCompleted {
		t.Errorf("state: got %q, want completed", store.checkedState)
	}
}

// TestUnsubscribeToken round-trips the credential and rejects the two ways
// it can be wrong: a different secret, and a token minted for something
// else that happens to share it.
func TestUnsubscribeToken(t *testing.T) {
	id := uuid.NewV4()
	token, err := SignUnsubscribeToken("secret", id)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := ParseUnsubscribeToken("secret", token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != id {
		t.Errorf("round trip: got %v, want %v", got, id)
	}
	if _, err := ParseUnsubscribeToken("another-secret", token); err == nil {
		t.Error("a token signed with a different secret must be rejected")
	}

	// Same secret, different purpose — the audience claim is what keeps a
	// playback token from doubling as an unsubscribe link.
	foreign, err := SignUnsubscribeToken("secret", id)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := ParseUnsubscribeToken("secret", foreign+"x"); err == nil {
		t.Error("a tampered token must be rejected")
	}
}

func TestUnsubscribeURL(t *testing.T) {
	url := UnsubscribeURL("https://webtor.io/", "secret", uuid.NewV4())
	if url == "" {
		t.Fatal("no url")
	}
	if want := "https://webtor.io/subscription/unsubscribe/"; len(url) <= len(want) || url[:len(want)] != want {
		t.Errorf("url: got %q, want it to start with %q", url, want)
	}
	if UnsubscribeURL("https://webtor.io", "", uuid.NewV4()) != "" {
		t.Error("without a secret there is no link to render")
	}
}

// --- regressions found in review ---

// A completed subscription is never polled again, so anything still pending
// when it closes is pending forever. The finale's releases arriving inside
// the batching window is exactly when this happens.
func TestCompletionFlushesPendingHitsFirst(t *testing.T) {
	sub := seasonSub()
	recent := time.Now().Add(-time.Hour) // well inside the 12h notify window
	sub.LastNotifiedAt = &recent
	store := &fakeStore{
		due: []models.ReleaseSubscription{*sub},
		// Everything has aired, and the show is done — the season closes.
		episodes: []models.EpisodeMetadata{episode(1, 60*24*time.Hour), episode(2, 53*24*time.Hour)},
		pending:  []models.ReleaseSubscriptionHit{{SubscriptionID: sub.ID, InfoHash: "aa"}},
	}
	mail := &fakeMailer{}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: false}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.updates) != 1 {
		t.Fatalf("update letters: got %d, want 1 — the last finds must go out before the row closes", len(mail.updates))
	}
	if len(store.notifiedHashes) != 1 {
		t.Errorf("hits marked: %v, want the pending one", store.notifiedHashes)
	}
	if len(mail.offs) != 1 || !mail.offs[0] {
		t.Errorf("completion notice: %v", mail.offs)
	}
}

// "Found nothing" and "could not ask" look the same in the result slice.
// Promoting to active on the second one makes the next poll read the whole
// back catalogue as new — a 40-line email for a season that has been out
// for months.
func TestBaselineStaysPendingWhenEverySourceFailed(t *testing.T) {
	sub := seasonSub()
	sub.State = models.ReleaseSubscriptionStatePendingBaseline
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(1, 7*24*time.Hour)},
	}
	search := &fakeSearch{err: errors.New("addon unreachable")}
	p := NewPoller(store, search, &fakeMailer{}, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if store.checkedState != models.ReleaseSubscriptionStatePendingBaseline {
		t.Errorf("state: got %q, want it to stay pending_baseline", store.checkedState)
	}
	if !store.checkedNext.After(time.Now()) {
		t.Error("the row was not rescheduled")
	}
}

// The same distinction the other way round: a baseline poll that reached a
// source and legitimately found nothing has done its job.
func TestBaselineCompletesWhenSourcesAnsweredEmpty(t *testing.T) {
	sub := seasonSub()
	sub.State = models.ReleaseSubscriptionStatePendingBaseline
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(1, 7*24*time.Hour)},
	}
	p := NewPoller(store, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if store.checkedState != models.ReleaseSubscriptionStateActive {
		t.Errorf("state: got %q, want active", store.checkedState)
	}
}

// A row that fails deterministically keeps its past-due next_check_at, and
// the queue is ordered by it — so without a reschedule it sits at the head
// of every batch forever, re-running its whole outbound search each time.
func TestFailedPollIsStillRescheduled(t *testing.T) {
	for _, tt := range []struct {
		name  string
		store func(sub *models.ReleaseSubscription) *fakeStore
	}{
		{
			name: "episode metadata unavailable",
			store: func(sub *models.ReleaseSubscription) *fakeStore {
				return &fakeStore{due: []models.ReleaseSubscription{*sub}, episodesErr: errors.New("db down")}
			},
		},
		{
			name: "the hit could not be stored",
			store: func(sub *models.ReleaseSubscription) *fakeStore {
				return &fakeStore{
					due:       []models.ReleaseSubscription{*sub},
					episodes:  []models.EpisodeMetadata{episode(1, 7*24*time.Hour)},
					insertErr: errors.New("value too long for type character varying(40)"),
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := seasonSub()
			store := tt.store(sub)
			search := &fakeSearch{byContentID: map[string][]stremio.StreamItem{
				"tt1190634:3:1": {{InfoHash: "aa", Title: "The.Boys.S03E01"}},
			}}
			p := NewPoller(store, search, &fakeMailer{}, fakeTier{}, fakeAiring{airing: true}, testConfig())

			if _, err := p.Run(context.Background()); err != nil {
				t.Fatalf("run: %v", err)
			}
			if store.checkedNext.IsZero() || !store.checkedNext.After(time.Now()) {
				t.Errorf("next check: %v, want a future time so the row leaves the head of the queue", store.checkedNext)
			}
		})
	}
}

// The row in hand was read before the letter went out, so its
// LastNotifiedAt is stale by exactly the event that matters. Reading it
// literally backs off a subscription on the run where it finally delivered.
func TestASubscriptionThatJustDeliveredIsNotBackedOff(t *testing.T) {
	sub := seasonSub()
	sub.CreatedAt = time.Now().Add(-40 * 24 * time.Hour) // long silent
	store := &fakeStore{
		due:      []models.ReleaseSubscription{*sub},
		episodes: []models.EpisodeMetadata{episode(5, 24*time.Hour)},
		pending:  []models.ReleaseSubscriptionHit{{SubscriptionID: sub.ID, InfoHash: "aa"}},
	}
	mail := &fakeMailer{}
	p := NewPoller(store, &fakeSearch{}, mail, fakeTier{}, fakeAiring{airing: true}, testConfig())

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.updates) != 1 {
		t.Fatalf("updates: got %d, want 1", len(mail.updates))
	}
	// Paid account with an episode inside the hot window: 3h, not the 24h
	// ceiling the stale idle counter would have produced.
	if d := time.Until(store.checkedNext); d > 4*time.Hour {
		t.Errorf("next check in %v, want the hot cadence — the show just delivered", d.Round(time.Minute))
	}
}

// The ceiling is a flag and can be misconfigured to zero; the retry path
// must not turn that into a row that is due again immediately.
func TestRetryDoesNotBecomeAHotLoopWithoutACeiling(t *testing.T) {
	cfg := testConfig()
	cfg.IntervalMax = 0
	sub := seasonSub()
	sub.User = nil // nothing to mail: the early-return retry path
	store := &fakeStore{due: []models.ReleaseSubscription{*sub}}
	p := NewPoller(store, &fakeSearch{}, &fakeMailer{}, fakeTier{}, fakeAiring{}, cfg)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !store.checkedNext.After(time.Now().Add(time.Hour)) {
		t.Errorf("next check: %v, want it comfortably in the future", store.checkedNext)
	}
}
