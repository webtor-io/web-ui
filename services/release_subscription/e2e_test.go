package release_subscription

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/stremio"
)

// The whole scenario, end to end: a user subscribes, gets a confirmation,
// the poller takes its silent first look, a new release turns up, the user
// gets one letter naming exactly that release, a quiet poll sends nothing,
// and the unsubscribe link from the letter ends it.
//
// Everything between the store and SMTP is the real thing — the real
// service, the real poller, the real notification service with the real
// templates and the real locale bundle. Two seams are faked, because
// neither can run in a unit test: the database (an in-memory store, whose
// SQL counterpart was verified against Postgres 17 by hand — see
// docs/release_subscriptions.md) and the outbound network (a stub search
// and a stub SMTP transport that keeps the messages).

// --- the in-memory database ---

// memStore backs both the service's store and the poller's pollStore, the
// way one Postgres backs both in production. The two hit-table rules it has
// to reproduce are the ones the schema enforces: an infohash is recorded
// once per subscription, and only rows that were never notified can reach a
// letter.
type memStore struct {
	mu   sync.Mutex
	subs map[uuid.UUID]*models.ReleaseSubscription
	hits map[uuid.UUID][]*models.ReleaseSubscriptionHit
	lang map[uuid.UUID]string
	// users is what the poller's query joins in: it mails an address, so a
	// row without an account is a row it cannot act on.
	users map[uuid.UUID]*models.User
	// the account-level stream settings a new subscription copies
	prefResolutions map[uuid.UUID][]string
	prefLang        map[uuid.UUID]string
	eps             []models.EpisodeMetadata
}

func newMemStore() *memStore {
	return &memStore{
		subs:            map[uuid.UUID]*models.ReleaseSubscription{},
		hits:            map[uuid.UUID][]*models.ReleaseSubscriptionHit{},
		lang:            map[uuid.UUID]string{},
		users:           map[uuid.UUID]*models.User{},
		prefResolutions: map[uuid.UUID][]string{},
		prefLang:        map[uuid.UUID]string{},
	}
}

func sameContent(s *models.ReleaseSubscription, kind, videoID string, season *int16) bool {
	if s.Kind != kind || s.VideoID != videoID {
		return false
	}
	a, b := -1, -1
	if s.Season != nil {
		a = int(*s.Season)
	}
	if season != nil {
		b = int(*season)
	}
	return a == b
}

func (m *memStore) Find(_ context.Context, userID uuid.UUID, kind, videoID string, season *int16) (*models.ReleaseSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subs {
		if s.UserID == userID && sameContent(s, kind, videoID, season) {
			return s, nil
		}
	}
	return nil, nil
}

func (m *memStore) FindByID(_ context.Context, id, userID uuid.UUID) (*models.ReleaseSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok && s.UserID == userID {
		return s, nil
	}
	return nil, nil
}

func (m *memStore) Get(_ context.Context, id uuid.UUID) (*models.ReleaseSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs[id], nil
}

func (m *memStore) Count(_ context.Context, userID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.subs {
		if s.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (m *memStore) List(_ context.Context, userID uuid.UUID) ([]models.ReleaseSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.ReleaseSubscription
	for _, s := range m.subs {
		if s.UserID == userID {
			out = append(out, *s)
		}
	}
	return out, nil
}

// Create mirrors the unique index over (user, kind, video, season): a second
// identical row is not an error, it just does not happen.
func (m *memStore) Create(ctx context.Context, sub *models.ReleaseSubscription) (bool, error) {
	if existing, _ := m.Find(ctx, sub.UserID, sub.Kind, sub.VideoID, sub.Season); existing != nil {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sub.ID == uuid.Nil {
		sub.ID = uuid.NewV4()
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	m.subs[sub.ID] = sub
	return true, nil
}

func (m *memStore) DeleteByContent(ctx context.Context, userID uuid.UUID, kind, videoID string, season *int16) (bool, error) {
	sub, _ := m.Find(ctx, userID, kind, videoID, season)
	if sub == nil {
		return false, nil
	}
	return true, m.DeleteByID(ctx, sub.ID)
}

func (m *memStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	sub, _ := m.FindByID(ctx, id, userID)
	if sub == nil {
		return nil
	}
	return m.DeleteByID(ctx, id)
}

// DeleteByID drops the subscription and its hits, as the foreign key does.
func (m *memStore) DeleteByID(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, id)
	delete(m.hits, id)
	return nil
}

func (m *memStore) SetEnabled(_ context.Context, id, userID uuid.UUID, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok && s.UserID == userID {
		s.Enabled = enabled
	}
	return nil
}

// --- the poller's half of the same store ---

func (m *memStore) ListDue(_ context.Context, now time.Time, limit int) ([]models.ReleaseSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.ReleaseSubscription
	for _, s := range m.subs {
		if s.Enabled && s.State != models.ReleaseSubscriptionStateCompleted && !s.NextCheckAt.After(now) {
			row := *s
			row.User = m.users[s.UserID] // the Relation("User") join
			out = append(out, row)
		}
	}
	return out, nil
}

// InsertHits is the ON CONFLICT DO NOTHING: a hash already recorded for this
// subscription is skipped, which is what keeps it out of a second letter.
func (m *memStore) InsertHits(_ context.Context, hits []models.ReleaseSubscriptionHit, baseline bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	added := 0
	for i := range hits {
		h := hits[i]
		seen := false
		for _, existing := range m.hits[h.SubscriptionID] {
			if existing.InfoHash == h.InfoHash {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		h.FirstSeenAt = now
		h.IsBaseline = baseline
		if baseline {
			stamp := now
			h.NotifiedAt = &stamp
		}
		m.hits[h.SubscriptionID] = append(m.hits[h.SubscriptionID], &h)
		added++
	}
	return added, nil
}

func (m *memStore) ListPendingHits(_ context.Context, id uuid.UUID) ([]models.ReleaseSubscriptionHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.ReleaseSubscriptionHit
	for _, h := range m.hits[id] {
		if h.NotifiedAt == nil {
			out = append(out, *h)
		}
	}
	return out, nil
}

func (m *memStore) MarkHitsNotified(_ context.Context, id uuid.UUID, infohashes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, h := range m.hits[id] {
		for _, want := range infohashes {
			if h.InfoHash == want && h.NotifiedAt == nil {
				stamp := now
				h.NotifiedAt = &stamp
			}
		}
	}
	return nil
}

func (m *memStore) MarkChecked(_ context.Context, id uuid.UUID, state string, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok {
		now := time.Now()
		s.LastCheckedAt = &now
		s.NextCheckAt = next
		s.State = state
	}
	return nil
}

func (m *memStore) MarkNotified(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok {
		now := time.Now()
		s.LastNotifiedAt = &now
	}
	return nil
}

func (m *memStore) SeasonEpisodes(context.Context, string, int16) ([]models.EpisodeMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eps, nil
}

func (m *memStore) UpsertMetadata(context.Context, models.ContentType, *models.VideoMetadata) error {
	return nil
}

func (m *memStore) StreamPrefs(_ context.Context, userID uuid.UUID) ([]string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prefResolutions[userID], m.prefLang[userID]
}

// The scenario's account has sources: the poller half of the test drives a
// scripted search, and the subscribe half must get past the backstop.
func (m *memStore) HasStreamSources(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (m *memStore) UpdatePreferences(_ context.Context, id, userID uuid.UUID, resolutions []string, lang *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok && s.UserID == userID {
		s.PreferredResolutions = resolutions
		s.PreferredLanguage = lang
	}
	return nil
}

func (m *memStore) AccountLang(_ context.Context, userID uuid.UUID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lang[userID]
}

// due drags a subscription's next check into the past, the way an hour of
// wall-clock would.
func (m *memStore) due(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subs[id]; ok {
		s.NextCheckAt = time.Now().Add(-time.Minute)
		if s.LastNotifiedAt != nil {
			long := s.LastNotifiedAt.Add(-48 * time.Hour)
			s.LastNotifiedAt = &long
		}
	}
}

// --- the stubbed outside world ---

// scriptedSearch answers with whatever the scenario has published so far.
type scriptedSearch struct {
	mu      sync.Mutex
	streams []stremio.StreamItem
	queries []string
}

func (s *scriptedSearch) Search(_ context.Context, _ *auth.User, _, contentID string) ([]stremio.StreamItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, contentID)
	return append([]stremio.StreamItem(nil), s.streams...), nil
}

func (s *scriptedSearch) publish(items ...stremio.StreamItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams = append(s.streams, items...)
}

// sentMail is the SMTP transport, minus the SMTP.
type sentMail struct {
	mu   sync.Mutex
	msgs []mailMessage
}

type mailMessage struct{ to, subject, body string }

func (m *sentMail) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, mailMessage{to: to, subject: subject, body: body})
	return nil
}

func (m *sentMail) all() []mailMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mailMessage(nil), m.msgs...)
}

func (m *sentMail) last() mailMessage {
	all := m.all()
	if len(all) == 0 {
		return mailMessage{}
	}
	return all[len(all)-1]
}

// memJournal is the notification table: what the 24-hour duplicate check
// reads. GetLastMailedByKeyAndUser only ever considers rows with MailedAt
// set, the same restriction the real SQL query applies — a row nobody ever
// mailed must not look like a duplicate.
type memJournal struct {
	mu   sync.Mutex
	rows []*models.Notification
}

func (j *memJournal) GetLastMailedByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID && r.MailedAt != nil {
			return r, nil
		}
	}
	return nil, nil
}

func (j *memJournal) MarkMailed(_ context.Context, id uuid.UUID, to string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, r := range j.rows {
		if r.NotificationID == id {
			r.MailedAt = &now
			return nil
		}
	}
	return nil
}

// GetLastByKeyAndUser is the feed guard's read: the newest row for this key
// and user whether or not it was ever mailed. No MailedAt condition here --
// that is the whole difference from the method above, and dropping it is
// what lets a redelivered event find its existing entry instead of adding a
// second one.
func (j *memJournal) GetLastByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID {
			return r, nil
		}
	}
	return nil, nil
}

func (j *memJournal) Create(_ context.Context, n *models.Notification) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n.NotificationID == (uuid.UUID{}) {
		n.NotificationID = uuid.NewV4()
	}
	n.CreatedAt = time.Now()
	j.rows = append(j.rows, n)
	return nil
}

// CountUnread, ListByUser, MarkAllRead and PruneKeepingNewest are not
// exercised by this suite -- it drives Send/SendExpiring end to end, not
// the feed-reading path -- but memJournal still has to satisfy
// notification.notificationStore (aliased notification.Store) to be usable
// with notification.NewWith.
func (j *memJournal) CountUnread(_ context.Context, userID uuid.UUID) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	count := 0
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (j *memJournal) ListByUser(_ context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []models.Notification
	for i := len(j.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if r := j.rows[i]; r.UserID != nil && *r.UserID == userID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (j *memJournal) MarkAllRead(_ context.Context, userID uuid.UUID) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			r.ReadAt = &now
		}
	}
	return nil
}

func (j *memJournal) PruneKeepingNewest(_ context.Context, _ int) error {
	return nil
}

// --- the scenario ---

func stream(hash, title, source string) stremio.StreamItem {
	return stremio.StreamItem{InfoHash: hash, Title: title, Name: source}
}

func TestSubscriptionScenarioEndToEnd(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	i18nSvc := i18n.New(locales.FS())

	mail := &sentMail{}
	ns := notification.NewWith(&memJournal{}, mail, i18nSvc, "https://webtor.io", "../../templates/notification")

	store := newMemStore()
	// Two episodes of the season have aired; the show is still in
	// production, so the subscription stays open.
	airedAt := time.Now().Add(-7 * 24 * time.Hour)
	store.eps = []models.EpisodeMetadata{
		{VideoID: "tt1190634", Season: 3, Episode: 1, AirDate: &airedAt},
		{VideoID: "tt1190634", Season: 3, Episode: 2, AirDate: &airedAt},
	}

	user := &auth.User{ID: uuid.NewV4(), Email: "viewer@example.com"}
	store.users[user.ID] = &models.User{UserID: user.ID, Email: user.Email}
	store.lang[user.ID] = "ru"

	svc := &Service{store: store, airing: subAiring{airing: true}, mail: ns, domain: "https://webtor.io", secret: "e2e-secret", sync: true}

	search := &scriptedSearch{}
	// What the user's sources already had when they subscribed.
	search.publish(stream("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "The.Boys.S03E01.1080p.WEB-DL", "Torrentio\n1080p"))

	cfg := testConfig()
	cfg.Domain = "https://webtor.io"
	cfg.Secret = "e2e-secret"
	poller := NewPoller(store, search, ns, fakeTier{}, subAiring{airing: true}, cfg)

	// 1. The user subscribes from the season selector.
	sub, added, err := svc.Subscribe(context.Background(), user, Request{
		Kind: "season", VideoID: "tt1190634", Season: 3, Source: "discover_season", Lang: "ru",
	}, FreeTierLimit)
	if err != nil || !added {
		t.Fatalf("subscribe: added=%v err=%v", added, err)
	}

	// ...and is told so, in their own language.
	if len(mail.all()) != 1 {
		t.Fatalf("letters after subscribing: got %d, want 1 (the confirmation)", len(mail.all()))
	}
	confirmation := mail.last()
	if confirmation.to != user.Email {
		t.Errorf("confirmation went to %q", confirmation.to)
	}
	if !strings.Contains(confirmation.subject, "Подписка оформлена") {
		t.Errorf("confirmation subject is not the Russian one: %q", confirmation.subject)
	}
	if !strings.Contains(confirmation.body, "Сезон 3") {
		t.Errorf("confirmation body does not name the season:\n%s", confirmation.body)
	}
	unsubscribeURL := extractUnsubscribeURL(confirmation.body)
	if unsubscribeURL == "" {
		t.Fatalf("confirmation carries no unsubscribe link:\n%s", confirmation.body)
	}

	// 2. First poll: a silent snapshot of what already existed.
	if _, err := poller.Run(context.Background()); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}
	if len(mail.all()) != 1 {
		t.Fatalf("the baseline poll sent a letter: %+v", mail.last())
	}
	if got := store.subs[sub.ID].State; got != models.ReleaseSubscriptionStateActive {
		t.Errorf("state after baseline: got %q, want active", got)
	}

	// 3. A new release turns up — and one the user already knows about
	//    turns up again, as sources routinely re-report.
	search.publish(
		stream("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "The.Boys.S03E01.1080p.WEB-DL", "Torrentio\n1080p"),
		stream("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "The.Boys.S03E03.2160p.WEB-DL", "RuTracker.org\n2160p"),
	)
	store.due(sub.ID)
	if _, err := poller.Run(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}

	// 4. One letter, naming the new release and nothing else.
	letters := mail.all()
	if len(letters) != 2 {
		t.Fatalf("letters after the new release: got %d, want 2", len(letters))
	}
	update := letters[1]
	if !strings.Contains(update.subject, "Новые раздачи") {
		t.Errorf("update subject: %q", update.subject)
	}
	if !strings.Contains(update.body, "The.Boys.S03E03.2160p.WEB-DL") {
		t.Errorf("update does not name the new release:\n%s", update.body)
	}
	if strings.Contains(update.body, "The.Boys.S03E01.1080p.WEB-DL") {
		t.Errorf("update names a release the user already had:\n%s", update.body)
	}
	if !strings.Contains(update.body, "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Errorf("update carries no playable link:\n%s", update.body)
	}
	if !strings.Contains(update.body, "RuTracker.org") {
		t.Errorf("update does not say where it came from:\n%s", update.body)
	}

	// 5. Nothing new since: the next poll is silent.
	store.due(sub.ID)
	if _, err := poller.Run(context.Background()); err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if len(mail.all()) != 2 {
		t.Fatalf("a poll with nothing new sent a letter: %+v", mail.last())
	}

	// 6. The link from the letter ends the subscription, with no login.
	token := unsubscribeURL[strings.LastIndex(unsubscribeURL, "/")+1:]
	removed, err := svc.DeleteByToken(context.Background(), token)
	if err != nil || removed == nil {
		t.Fatalf("unsubscribe by token: removed=%v err=%v", removed, err)
	}
	if _, ok := store.subs[sub.ID]; ok {
		t.Error("the subscription survived its own unsubscribe link")
	}

	// 7. And a poll after that has nothing to poll.
	before := len(search.queries)
	if _, err := poller.Run(context.Background()); err != nil {
		t.Fatalf("final poll: %v", err)
	}
	if len(search.queries) != before {
		t.Error("a removed subscription was still being searched for")
	}
}

// TestScenarioFreeTierCapStopsAtThree walks the cap the way a user meets
// it: three subscriptions land, the fourth is refused, and nothing about
// the fourth reaches the database or the mailbox.
func TestScenarioFreeTierCapStopsAtThree(t *testing.T) {
	store := newMemStore()
	mail := &subMail{}
	svc := &Service{store: store, airing: subAiring{airing: true}, mail: mail, sync: true}
	user := &auth.User{ID: uuid.NewV4(), Email: "viewer@example.com"}

	for i, id := range []string{"tt0000001", "tt0000002", "tt0000003"} {
		_, added, err := svc.Subscribe(context.Background(), user, Request{Kind: "movie", VideoID: id}, FreeTierLimit)
		if err != nil || !added {
			t.Fatalf("subscription %d: added=%v err=%v", i+1, added, err)
		}
	}

	_, _, err := svc.Subscribe(context.Background(), user, Request{Kind: "movie", VideoID: "tt0000004"}, FreeTierLimit)
	if err == nil {
		t.Fatal("the fourth subscription was accepted")
	}
	if n, _ := store.Count(context.Background(), user.ID); n != 3 {
		t.Errorf("rows: got %d, want 3", n)
	}
	if len(mail.on) != 3 {
		t.Errorf("confirmations: got %d, want 3", len(mail.on))
	}
}

func extractUnsubscribeURL(body string) string {
	const marker = "https://webtor.io/subscription/unsubscribe/"
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.IndexAny(rest, "\"'<> \n"); j > 0 {
		return rest[:j]
	}
	return rest
}
