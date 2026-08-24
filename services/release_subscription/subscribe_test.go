package release_subscription

import (
	"context"
	"errors"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/notification"
)

// The write path: who may hold a subscription, what a second click does,
// and which letter goes out when. The rules live here rather than in the
// handler, so this is where they are pinned.

type subStore struct {
	found     *models.ReleaseSubscription
	findErr   error
	count     int
	countErr  error
	created   *models.ReleaseSubscription
	createOK  bool
	createErr error
	list      []models.ReleaseSubscription

	removedByContent bool
	deletedIDs       []uuid.UUID
	enabledCalls     int
	accountLang      string
	// noSources flips HasStreamSources to false; the zero value keeps the
	// pre-existing tests on an account that has sources.
	noSources        bool
	sourcesErr       error
	prefResolutions  []string
	prefLang         string
	savedResolutions []string
	savedLang        *string
	cachedMetadata   *models.VideoMetadata
	cachedType       models.ContentType
}

func (f *subStore) AccountLang(context.Context, uuid.UUID) string { return f.accountLang }

func (f *subStore) HasStreamSources(context.Context, uuid.UUID) (bool, error) {
	return !f.noSources, f.sourcesErr
}

func (f *subStore) UpsertMetadata(_ context.Context, ct models.ContentType, md *models.VideoMetadata) error {
	f.cachedMetadata = md
	f.cachedType = ct
	return nil
}

func (f *subStore) StreamPrefs(context.Context, uuid.UUID) ([]string, string) {
	return f.prefResolutions, f.prefLang
}

func (f *subStore) UpdatePreferences(_ context.Context, _, _ uuid.UUID, resolutions []string, lang *string) error {
	f.savedResolutions = resolutions
	f.savedLang = lang
	return nil
}

func (f *subStore) Find(context.Context, uuid.UUID, string, string, *int16) (*models.ReleaseSubscription, error) {
	return f.found, f.findErr
}

func (f *subStore) FindByID(context.Context, uuid.UUID, uuid.UUID) (*models.ReleaseSubscription, error) {
	return f.found, f.findErr
}

func (f *subStore) Get(context.Context, uuid.UUID) (*models.ReleaseSubscription, error) {
	return f.found, f.findErr
}

func (f *subStore) Count(context.Context, uuid.UUID) (int, error) { return f.count, f.countErr }

func (f *subStore) List(context.Context, uuid.UUID) ([]models.ReleaseSubscription, error) {
	return f.list, nil
}

func (f *subStore) Create(_ context.Context, sub *models.ReleaseSubscription) (bool, error) {
	f.created = sub
	if f.createErr != nil {
		return false, f.createErr
	}
	return f.createOK, nil
}

func (f *subStore) DeleteByContent(context.Context, uuid.UUID, string, string, *int16) (bool, error) {
	return f.removedByContent, nil
}

func (f *subStore) Delete(_ context.Context, id, _ uuid.UUID) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func (f *subStore) DeleteByID(_ context.Context, id uuid.UUID) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func (f *subStore) SetEnabled(context.Context, uuid.UUID, uuid.UUID, bool) error {
	f.enabledCalls++
	return nil
}

type subMail struct {
	on  []notification.SubscriptionView
	off []notification.SubscriptionView
}

func (m *subMail) SendSubscriptionOn(_ string, _ uuid.UUID, sub notification.SubscriptionView) error {
	m.on = append(m.on, sub)
	return nil
}

func (m *subMail) SendSubscriptionOff(_ string, _ uuid.UUID, sub notification.SubscriptionView, _ bool) error {
	m.off = append(m.off, sub)
	return nil
}

type subAiring struct{ airing bool }

func (f subAiring) IsAiringSeries(context.Context, string) bool { return f.airing }

// The poller's checked variant — the e2e scenario hands the same fake to
// both halves of the feature.
func (f subAiring) IsAiringSeriesChecked(context.Context, string) (bool, error) {
	return f.airing, nil
}

type subMeta struct {
	md  *models.VideoMetadata
	err error
}

func (f subMeta) LookupByVideoID(context.Context, string, models.ContentType) (*models.VideoMetadata, error) {
	return f.md, f.err
}

func newTestService(st store, mail Mailer, airing bool) *Service {
	s := &Service{store: st, mail: mail, domain: "https://webtor.io", secret: "secret", sync: true}
	s.airing = subAiring{airing: airing}
	return s
}

var viewer = &auth.User{ID: uuid.NewV4(), Email: "viewer@example.com"}

func movieReq() Request {
	return Request{Kind: "movie", VideoID: "tt0111161", Source: "empty_streams", Lang: "ru"}
}

func seasonReq() Request {
	return Request{Kind: "season", VideoID: "tt1190634", Season: 3, Source: "discover_season", Lang: "en"}
}

func TestSubscribeCreatesAndConfirms(t *testing.T) {
	st := &subStore{createOK: true}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	sub, added, err := s.Subscribe(context.Background(), viewer, seasonReq(), -1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !added || sub == nil {
		t.Fatalf("added=%v sub=%v, want a fresh row", added, sub)
	}
	if st.created.Kind != models.ReleaseSubscriptionKindSeason || st.created.GetSeason() != 3 {
		t.Errorf("stored row: %+v", st.created)
	}
	if st.created.State != models.ReleaseSubscriptionStatePendingBaseline {
		t.Errorf("state: got %q, want pending_baseline — the first poll must be a silent snapshot", st.created.State)
	}
	if st.created.Lang != "en" || st.created.Source != "discover_season" {
		t.Errorf("lang/source not captured: %q / %q", st.created.Lang, st.created.Source)
	}
	if len(mail.on) != 1 {
		t.Fatalf("confirmations sent: got %d, want 1", len(mail.on))
	}
	if mail.on[0].UnsubscribeURL == "" {
		t.Error("the confirmation carries no unsubscribe link")
	}
}

// A padded id passes validation (it trims before checking) but must be
// persisted trimmed: the raw string once went into the row verbatim, where
// it polled sources for " tt0111161", matched nothing forever, and could
// coexist with a clean duplicate the unique index reads as different.
func TestSubscribeStoresTrimmedVideoID(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, true)

	req := movieReq()
	req.VideoID = " tt0111161\n"
	if _, _, err := s.Subscribe(context.Background(), viewer, req, -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if st.created.VideoID != "tt0111161" {
		t.Errorf("stored video id: %q, want it trimmed", st.created.VideoID)
	}
}

// An account with no addons and no indexers holds a subscription that can
// never fire — the poller runs the user's own pipeline. The entry points
// hide the offer client-side; this is the backstop for the paths that
// don't.
func TestSubscribeRefusesWithoutStreamSources(t *testing.T) {
	st := &subStore{createOK: true, noSources: true}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	_, _, err := s.Subscribe(context.Background(), viewer, movieReq(), FreeTierLimit)
	if !errors.Is(err, ErrNoStreamSources) {
		t.Fatalf("subscribe: got %v, want ErrNoStreamSources", err)
	}
	if st.created != nil {
		t.Error("no row may be written for an account with nothing to poll")
	}
	if len(mail.on) != 0 {
		t.Error("no confirmation for a refused subscription")
	}
}

// The check fails open: a flaky count query must not refuse a subscription
// the UI just offered.
func TestSubscribeAllowsWhenSourceCheckErrors(t *testing.T) {
	st := &subStore{createOK: true, noSources: true, sourcesErr: errors.New("db timeout")}
	s := newTestService(st, &subMail{}, true)

	if _, added, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil || !added {
		t.Fatalf("subscribe: added=%v err=%v, want the fail-open branch to accept", added, err)
	}
}

// Clicking a bell that is already filled must not produce a second row, and
// must not produce a second letter either.
func TestSubscribeTwiceIsQuiet(t *testing.T) {
	existing := &models.ReleaseSubscription{ID: uuid.NewV4(), Kind: models.ReleaseSubscriptionKindMovie, VideoID: "tt0111161"}
	st := &subStore{found: existing}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	sub, added, err := s.Subscribe(context.Background(), viewer, movieReq(), -1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if added {
		t.Error("added=true for a subscription that already existed")
	}
	if sub != existing {
		t.Error("the existing row was not returned")
	}
	if st.created != nil {
		t.Error("a second row was written")
	}
	if len(mail.on) != 0 {
		t.Error("a second confirmation was sent")
	}
}

// Losing the insert race means someone else's identical row won. The caller
// still gets a subscription to render, and no letter goes out for a row this
// call did not create.
func TestSubscribeLosingTheRaceReadsBackTheWinner(t *testing.T) {
	winner := &models.ReleaseSubscription{ID: uuid.NewV4()}
	st := &subRaceStore{subStore: subStore{createOK: false}, second: winner}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	sub, added, err := s.Subscribe(context.Background(), viewer, movieReq(), -1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if added || sub != winner {
		t.Errorf("added=%v sub=%v, want the winning row and added=false", added, sub)
	}
	if len(mail.on) != 0 {
		t.Error("a confirmation went out for a row this call did not create")
	}
}

// subRaceStore answers the first Find with nothing and the second with the row
// that won the insert.
type subRaceStore struct {
	subStore
	second *models.ReleaseSubscription
	calls  int
}

func (s *subRaceStore) Find(context.Context, uuid.UUID, string, string, *int16) (*models.ReleaseSubscription, error) {
	s.calls++
	if s.calls == 1 {
		return nil, nil
	}
	return s.second, nil
}

func TestSubscribeEnforcesTheFreeTierCap(t *testing.T) {
	st := &subStore{count: FreeTierLimit}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	_, _, err := s.Subscribe(context.Background(), viewer, movieReq(), FreeTierLimit)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error: got %v, want ErrLimitExceeded", err)
	}
	if st.created != nil {
		t.Error("a row was written past the cap")
	}
	if len(mail.on) != 0 {
		t.Error("a letter went out for a refused subscription")
	}
}

// -1 is what a paid account is given: the count query is not even run.
func TestSubscribeUnlimitedSkipsTheCount(t *testing.T) {
	st := &subStore{count: 500, createOK: true}
	s := newTestService(st, &subMail{}, true)
	if _, _, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if st.created == nil {
		t.Error("a paid account was capped")
	}
}

func TestSubscribeRefusesAFinishedSeason(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, false)

	_, _, err := s.Subscribe(context.Background(), viewer, seasonReq(), -1)
	if !errors.Is(err, ErrNotEligible) {
		t.Fatalf("error: got %v, want ErrNotEligible", err)
	}
	if st.created != nil {
		t.Error("a row was written for a season with no future")
	}
}

// A movie needs no eligibility check — nothing local knows whether a release
// is coming, and that judgement is the user's.
func TestSubscribeMovieNeedsNoAiringCheck(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, false)
	if _, added, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil || !added {
		t.Fatalf("added=%v err=%v, want a created movie subscription", added, err)
	}
}

func TestSubscribeRejectsMalformedRequests(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, true)
	for _, req := range []Request{
		{Kind: "season", VideoID: "tt1190634"},                // no season
		{Kind: "movie", VideoID: "wt-8c1f"},                   // library id
		{Kind: "series", VideoID: "tt1190634"},                // unknown kind
		{Kind: "season", VideoID: "tt1190634:3:4", Season: 3}, // episode id
	} {
		if _, _, err := s.Subscribe(context.Background(), viewer, req, -1); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: got %v, want ErrBadRequest", req, err)
		}
	}
	if st.created != nil {
		t.Error("a malformed request reached the database")
	}
}

func TestSubscribeSnapshotsTitleAndPoster(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, true)
	s.meta = subMeta{md: &models.VideoMetadata{Title: "The Boys", PosterURL: "https://img/poster.jpg"}}

	if _, _, err := s.Subscribe(context.Background(), viewer, seasonReq(), -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if st.created.Title == nil || *st.created.Title != "The Boys" {
		t.Errorf("title: %v", st.created.Title)
	}
	if st.created.PosterURL == nil || *st.created.PosterURL != "https://img/poster.jpg" {
		t.Errorf("poster: %v", st.created.PosterURL)
	}
	// The profile serves posters from our own endpoint, which resolves them
	// out of the metadata tables — so subscribing has to leave a row there.
	if st.cachedMetadata == nil || st.cachedType != models.ContentTypeSeries {
		t.Errorf("metadata was not cached for the poster endpoint: %v / %q", st.cachedMetadata, st.cachedType)
	}
}

// A metadata lookup that fails must not fail the subscription: the row just
// renders as its video id until something fills it in.
func TestSubscribeSurvivesAMetadataFailure(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, true)
	s.meta = subMeta{err: errors.New("tmdb down")}

	if _, added, err := s.Subscribe(context.Background(), viewer, seasonReq(), -1); err != nil || !added {
		t.Fatalf("added=%v err=%v, want the row created anyway", added, err)
	}
	if st.created.Title != nil {
		t.Error("a failed lookup wrote a title")
	}
}

func TestUnsubscribeSendsTheNotice(t *testing.T) {
	existing := &models.ReleaseSubscription{ID: uuid.NewV4(), Title: strptr("The Boys"), Season: int16ptr(3), Kind: models.ReleaseSubscriptionKindSeason}
	st := &subStore{found: existing, removedByContent: true}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	removed, err := s.Unsubscribe(context.Background(), viewer, seasonReq())
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if len(mail.off) != 1 {
		t.Fatalf("notices sent: got %d, want 1", len(mail.off))
	}
	// The row is read before the delete — afterwards there is nothing left
	// to name the letter with.
	if mail.off[0].Title != "The Boys" || mail.off[0].Season != 3 {
		t.Errorf("notice: %+v", mail.off[0])
	}
}

func TestUnsubscribeNothingSendsNothing(t *testing.T) {
	st := &subStore{removedByContent: false}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	removed, err := s.Unsubscribe(context.Background(), viewer, movieReq())
	if err != nil || removed {
		t.Fatalf("removed=%v err=%v, want false", removed, err)
	}
	if len(mail.off) != 0 {
		t.Error("a notice went out for a subscription that was not there")
	}
}

func TestDeleteByIDSendsTheNotice(t *testing.T) {
	id := uuid.NewV4()
	st := &subStore{found: &models.ReleaseSubscription{ID: id, Title: strptr("Dune")}}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	if err := s.Delete(context.Background(), viewer, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(st.deletedIDs) != 1 || st.deletedIDs[0] != id {
		t.Errorf("deleted: %v", st.deletedIDs)
	}
	if len(mail.off) != 1 {
		t.Errorf("notices: got %d, want 1", len(mail.off))
	}
}

func TestDeleteOfSomethingGoneIsQuiet(t *testing.T) {
	st := &subStore{found: nil}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	if err := s.Delete(context.Background(), viewer, uuid.NewV4()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(st.deletedIDs) != 0 || len(mail.off) != 0 {
		t.Error("a missing row produced a delete or a letter")
	}
}

// The one-click link from an email: the signed token is the whole
// authorization, and clicking it twice must read as done rather than fail.
func TestDeleteByToken(t *testing.T) {
	id := uuid.NewV4()
	st := &subStore{found: &models.ReleaseSubscription{ID: id, Title: strptr("The Boys")}}
	s := newTestService(st, &subMail{}, true)

	token, err := SignUnsubscribeToken("secret", id)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sub, err := s.DeleteByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("delete by token: %v", err)
	}
	if sub == nil || sub.ID != id {
		t.Fatalf("returned: %v", sub)
	}
	if len(st.deletedIDs) != 1 {
		t.Errorf("deleted: %v", st.deletedIDs)
	}

	// Second click: the row is gone, and that is the outcome the reader
	// wanted — not an error.
	st.found = nil
	sub, err = s.DeleteByToken(context.Background(), token)
	if err != nil || sub != nil {
		t.Errorf("second click: sub=%v err=%v, want (nil, nil)", sub, err)
	}
}

// The GET half of the unsubscribe flow: a mail scanner prefetching the link
// must change nothing. Peek resolves the token to a name for the confirm
// page and deletes nothing — the deletion is the POST's, behind the button.
func TestPeekByTokenDeletesNothing(t *testing.T) {
	id := uuid.NewV4()
	st := &subStore{found: &models.ReleaseSubscription{ID: id, Title: strptr("The Boys")}}
	s := newTestService(st, &subMail{}, true)

	token, err := SignUnsubscribeToken("secret", id)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sub, err := s.PeekByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if sub == nil || sub.ID != id {
		t.Fatalf("returned: %v", sub)
	}
	if len(st.deletedIDs) != 0 {
		t.Errorf("a peek deleted rows: %v", st.deletedIDs)
	}

	if _, err := s.PeekByToken(context.Background(), "not-a-token"); err == nil {
		t.Error("a garbage token was accepted")
	}
}

func TestDeleteByTokenRejectsAForgedToken(t *testing.T) {
	st := &subStore{found: &models.ReleaseSubscription{ID: uuid.NewV4()}}
	s := newTestService(st, &subMail{}, true)

	token, _ := SignUnsubscribeToken("someone-elses-secret", uuid.NewV4())
	if _, err := s.DeleteByToken(context.Background(), token); err == nil {
		t.Error("a token signed with another secret was accepted")
	}
	if len(st.deletedIDs) != 0 {
		t.Error("a forged token deleted a row")
	}
}

// Without SMTP configured the service is handed no mailer at all; every
// path still has to work, silently.
func TestNoMailerIsNotAFailure(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, nil, true)
	if _, added, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil || !added {
		t.Fatalf("added=%v err=%v", added, err)
	}
	st.found = &models.ReleaseSubscription{ID: uuid.NewV4()}
	st.removedByContent = true
	if _, err := s.Unsubscribe(context.Background(), viewer, movieReq()); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
}

func strptr(s string) *string { return &s }
func int16ptr(n int16) *int16 { return &n }

// The account's current language wins over the one captured when the
// subscription was created — otherwise a user who switched to English would
// keep getting Russian notices about subscriptions made before the switch,
// while their update letters (sent from the poller, which has always read
// the account) arrived in English. One letter, one language.
func TestLettersFollowTheAccountLanguage(t *testing.T) {
	st := &subStore{createOK: true, accountLang: "de"}
	mail := &subMail{}
	s := newTestService(st, mail, true)

	req := movieReq() // captured lang: "ru"
	if _, _, err := s.Subscribe(context.Background(), viewer, req, -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if len(mail.on) != 1 || mail.on[0].Lang != "de" {
		t.Errorf("confirmation language: got %q, want the account's \"de\"", mail.on[0].Lang)
	}

	st.found = &models.ReleaseSubscription{ID: uuid.NewV4(), Lang: "ru"}
	st.removedByContent = true
	if _, err := s.Unsubscribe(context.Background(), viewer, req); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if len(mail.off) != 1 || mail.off[0].Lang != "de" {
		t.Errorf("removal notice language: got %q, want the account's \"de\"", mail.off[0].Lang)
	}
}

// An account that has never been seen since the language column existed
// falls back to what the subscription itself captured.
func TestLettersFallBackToTheSubscriptionLanguage(t *testing.T) {
	st := &subStore{createOK: true} // no account language
	mail := &subMail{}
	s := newTestService(st, mail, true)

	if _, _, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if len(mail.on) != 1 || mail.on[0].Lang != "ru" {
		t.Errorf("confirmation language: got %q, want the captured \"ru\"", mail.on[0].Lang)
	}
}

// A new subscription starts as a copy of the account's stream settings, so
// the first thing it does matches what the user already streams. From then
// on the two are independent — only the profile row edits the subscription.
func TestSubscribeSnapshotsAccountStreamPreferences(t *testing.T) {
	st := &subStore{createOK: true, prefResolutions: []string{"1080p", "720p"}, prefLang: "ru"}
	s := newTestService(st, &subMail{}, true)

	if _, _, err := s.Subscribe(context.Background(), viewer, seasonReq(), -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got := st.created.PreferredResolutions; len(got) != 2 || got[0] != "1080p" {
		t.Errorf("resolutions: got %v, want the account's", got)
	}
	if st.created.GetPreferredLanguage() != "ru" {
		t.Errorf("language: got %q, want \"ru\"", st.created.GetPreferredLanguage())
	}
}

// An account with nothing narrowed subscribes to everything, and the row
// says so by holding no preferences at all rather than a copy of the full
// vocabulary.
func TestSubscribeWithoutAccountPreferencesStoresNone(t *testing.T) {
	st := &subStore{createOK: true}
	s := newTestService(st, &subMail{}, true)

	if _, _, err := s.Subscribe(context.Background(), viewer, movieReq(), -1); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if st.created.PreferredResolutions != nil || st.created.PreferredLanguage != nil {
		t.Errorf("preferences: got %v/%v, want none", st.created.PreferredResolutions, st.created.PreferredLanguage)
	}
}

func TestSetPreferences(t *testing.T) {
	st := &subStore{}
	s := newTestService(st, &subMail{}, true)

	if err := s.SetPreferences(context.Background(), viewer.ID, uuid.NewV4(), []string{"4k"}, "de"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(st.savedResolutions) != 1 || st.savedResolutions[0] != "4k" {
		t.Errorf("resolutions: %v", st.savedResolutions)
	}
	if st.savedLang == nil || *st.savedLang != "de" {
		t.Errorf("language: %v", st.savedLang)
	}

	// "Any language" is stored as nothing, not as an empty string, so the
	// poller's "no preference" branch is the one that runs.
	if err := s.SetPreferences(context.Background(), viewer.ID, uuid.NewV4(), nil, "  "); err != nil {
		t.Fatalf("set: %v", err)
	}
	if st.savedLang != nil {
		t.Errorf("language: got %v, want nil for \"any\"", *st.savedLang)
	}
}
