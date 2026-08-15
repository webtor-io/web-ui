package release_subscription

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	rs "github.com/webtor-io/web-ui/services/release_subscription"
)

// What is tested here is the HTTP contract the Discover client is written
// against: which refusal becomes which status code, and what shape comes
// back. The client's side of the same contract is covered in
// assets/src/js/lib/discover/subscriptionsClient.test.js — the two have to
// agree on 402 / 409 / 400 and on the field names.

type fakeService struct {
	subscribeErr error
	subscribed   *models.ReleaseSubscription
	added        bool
	lastRequest  rs.Request
	lastLimit    int

	removed        bool
	unsubscribeErr error

	list    []models.ReleaseSubscription
	deleted []uuid.UUID
	enabled map[uuid.UUID]bool
	listErr error
}

func (f *fakeService) Subscribe(_ context.Context, _ *auth.User, req rs.Request, limit int) (*models.ReleaseSubscription, bool, error) {
	f.lastRequest = req
	f.lastLimit = limit
	if f.subscribeErr != nil {
		return nil, false, f.subscribeErr
	}
	return f.subscribed, f.added, nil
}

func (f *fakeService) Unsubscribe(_ context.Context, _ *auth.User, req rs.Request) (bool, error) {
	f.lastRequest = req
	return f.removed, f.unsubscribeErr
}

func (f *fakeService) List(context.Context, uuid.UUID) ([]models.ReleaseSubscription, error) {
	return f.list, f.listErr
}

func (f *fakeService) Delete(_ context.Context, _ *auth.User, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeService) SetEnabled(_ context.Context, _, id uuid.UUID, enabled bool) error {
	if f.enabled == nil {
		f.enabled = map[uuid.UUID]bool{}
	}
	f.enabled[id] = enabled
	return nil
}

func (f *fakeService) DeleteByToken(context.Context, string) (*models.ReleaseSubscription, error) {
	return nil, nil
}

var testUser = &models.User{UserID: uuid.NewV4(), Email: "viewer@example.com"}

// do mounts the routes on a bare engine and runs one request as a signed-in
// user. The user rides in the request context under auth.UserContext, which
// is what auth.GetUserFromContext reads once a token parameter is present.
func do(t *testing.T, svc subscriptions, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{svc: svc}
	h.routes(r)

	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	req := httptest.NewRequest(method, path+sep+"token=test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContext{}, testUser))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, w.Body.String())
	}
	return body
}

func TestSubscribeAccepted(t *testing.T) {
	season := int16(3)
	svc := &fakeService{
		added: true,
		subscribed: &models.ReleaseSubscription{
			ID: uuid.NewV4(), Kind: models.ReleaseSubscriptionKindSeason,
			VideoID: "tt1190634", Season: &season, State: models.ReleaseSubscriptionStatePendingBaseline, Enabled: true,
		},
	}
	w := do(t, svc, http.MethodPost, "/discover/subscriptions",
		`{"kind":"season","video_id":"tt1190634","season":3,"source":"discover_season"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["added"] != true {
		t.Errorf("added: got %v, want true", body["added"])
	}
	// The item comes back so the client can render the new state without a
	// second round-trip.
	item, ok := body["item"].(map[string]any)
	if !ok {
		t.Fatalf("no item in response: %s", w.Body.String())
	}
	if item["kind"] != "season" || item["video_id"] != "tt1190634" || item["season"] != float64(3) {
		t.Errorf("item: got %v", item)
	}
	if svc.lastRequest.Source != "discover_season" {
		t.Errorf("source not passed through: %q", svc.lastRequest.Source)
	}
}

// Each refusal has to arrive as its own status code: the client branches on
// them (upgrade prompt vs "nothing to wait for" vs generic failure).
func TestSubscribeRefusals(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{"free tier cap", rs.ErrLimitExceeded, http.StatusPaymentRequired, "limit_exceeded"},
		{"season already over", rs.ErrNotEligible, http.StatusConflict, "not_eligible"},
		{"malformed request", errors.Wrap(rs.ErrBadRequest, "bad id"), http.StatusBadRequest, "bad_request"},
		{"anything else", errors.New("db is on fire"), http.StatusInternalServerError, "internal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := do(t, &fakeService{subscribeErr: tt.err}, http.MethodPost, "/discover/subscriptions",
				`{"kind":"movie","video_id":"tt0111161"}`)
			if w.Code != tt.wantCode {
				t.Fatalf("status: got %d, want %d (%s)", w.Code, tt.wantCode, w.Body.String())
			}
			if code := decode(t, w)["code"]; code != tt.wantBody {
				t.Errorf("code: got %v, want %q", code, tt.wantBody)
			}
		})
	}
}

func TestSubscribeRejectsGarbageJSON(t *testing.T) {
	w := do(t, &fakeService{}, http.MethodPost, "/discover/subscriptions", `not json at all`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

// An anonymous request must not reach the service at all — auth.HasAuth is
// what stops it, and the route group is where that is wired.
func TestJSONRoutesRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := &fakeService{}
	(&Handler{svc: svc}).routes(r)

	req := httptest.NewRequest(http.MethodPost, "/discover/subscriptions",
		strings.NewReader(`{"kind":"movie","video_id":"tt1"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", w.Code)
	}
	if svc.lastRequest.VideoID != "" {
		t.Error("an anonymous request reached the service")
	}
}

func TestUnsubscribeSeasonCarriesTheSeason(t *testing.T) {
	svc := &fakeService{removed: true}
	w := do(t, svc, http.MethodDelete, "/discover/subscriptions/season/tt1190634?season=3", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if decode(t, w)["removed"] != true {
		t.Error("removed flag missing")
	}
	if svc.lastRequest.Kind != "season" || svc.lastRequest.VideoID != "tt1190634" || svc.lastRequest.Season != 3 {
		t.Errorf("request: got %+v", svc.lastRequest)
	}
}

func TestUnsubscribeMovieHasNoSeason(t *testing.T) {
	svc := &fakeService{removed: true}
	do(t, svc, http.MethodDelete, "/discover/subscriptions/movie/tt0111161", "")
	if svc.lastRequest.Season != 0 {
		t.Errorf("season: got %d, want 0", svc.lastRequest.Season)
	}
}

func TestUnsubscribeRejectsNonNumericSeason(t *testing.T) {
	svc := &fakeService{}
	w := do(t, svc, http.MethodDelete, "/discover/subscriptions/season/tt1?season=abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if svc.lastRequest.VideoID != "" {
		t.Error("a malformed season still reached the service")
	}
}

// The id endpoint is what fills the bells on page load: one row per
// subscription, keyed by content.
func TestListIDs(t *testing.T) {
	season := int16(3)
	title := "The Boys"
	svc := &fakeService{list: []models.ReleaseSubscription{
		{ID: uuid.NewV4(), Kind: models.ReleaseSubscriptionKindSeason, VideoID: "tt1190634", Season: &season, Title: &title, Enabled: true, State: models.ReleaseSubscriptionStateActive},
		{ID: uuid.NewV4(), Kind: models.ReleaseSubscriptionKindMovie, VideoID: "tt0111161", Enabled: false, State: models.ReleaseSubscriptionStateActive},
	}}
	w := do(t, svc, http.MethodGet, "/discover/subscriptions/ids", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := decode(t, w)
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items: got %d, want 2", len(items))
	}
	if body["count"] != float64(2) {
		t.Errorf("count: got %v, want 2", body["count"])
	}
	first, _ := items[0].(map[string]any)
	if first["season"] != float64(3) || first["title"] != "The Boys" {
		t.Errorf("first item: %v", first)
	}
	// A movie carries no season at all rather than a zero the client would
	// have to special-case.
	second, _ := items[1].(map[string]any)
	if _, has := second["season"]; has {
		t.Errorf("movie item carries a season: %v", second)
	}
}

func TestListInternalError(t *testing.T) {
	w := do(t, &fakeService{listErr: errors.New("db down")}, http.MethodGet, "/discover/subscriptions", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

// The profile list form: staged deletions apply, and only the toggles that
// actually moved are written.
func TestFormUpdateAppliesDeletionsAndChangedTogglesOnly(t *testing.T) {
	keep := uuid.NewV4()
	flip := uuid.NewV4()
	drop := uuid.NewV4()
	svc := &fakeService{list: []models.ReleaseSubscription{
		{ID: keep, Enabled: true},
		{ID: flip, Enabled: true},
	}}

	form := url.Values{}
	form.Set("deleted_subscriptions", drop.String())
	form.Set("subscription_"+keep.String()+"_enabled", "on")
	// flip's checkbox is absent — an unchecked box is not submitted.

	gin.SetMode(gin.TestMode)
	r := gin.New()
	(&Handler{svc: svc}).routes(r)
	req := httptest.NewRequest(http.MethodPost, "/subscription/update?token=test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContext{}, testUser))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if len(svc.deleted) != 1 || svc.deleted[0] != drop {
		t.Errorf("deleted: got %v, want [%v]", svc.deleted, drop)
	}
	if _, touched := svc.enabled[keep]; touched {
		t.Error("a row whose toggle did not move was written anyway")
	}
	if on, ok := svc.enabled[flip]; !ok || on {
		t.Errorf("flipped row: got %v/%v, want written as disabled", on, ok)
	}
}
