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

func (m *subMail) SendSubscriptionOn(_ string, sub notification.SubscriptionView) error {
	m.on = append(m.on, sub)
	return nil
}

func (m *subMail) SendSubscriptionOff(_ string, sub notification.SubscriptionView, _ bool) error {
	m.off = append(m.off, sub)
	return nil
}

type subAiring struct{ airing bool }

func (f subAiring) IsAiringSeries(context.Context, string) bool { return f.airing }

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
