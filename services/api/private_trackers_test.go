package api

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

const privateTrackersTestUID = "76543210fedcba9876543210fedcba9876543210"

// answering returns an Api whose rest-api answers every request with status
// and body.
func answering(t *testing.T, status int, body string) *Api {
	t.Helper()
	return restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// Both calls go to the uid's path, the uid escaped into one segment, with
// web-ui's own claims, no user and no query, and read rest-api's answers whole.
func TestPrivateTrackersInternalClaims(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	s := restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		mu.Unlock()
		c := claimsOf(t, r)
		if c["internal"] != true {
			t.Errorf("%s: internal = %v, want true", r.Method, c["internal"])
		}
		if _, ok := c["uid"]; ok {
			t.Errorf("%s: X-Token carries uid %v", r.Method, c["uid"])
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"deleted":3}`))
			return
		}
		_, _ = w.Write([]byte(`[{"resource_id":"` + cacheTestHash + `","hosts":["tracker.example","b.invalid"],"source":"upload",` +
			`"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T11:00:00Z"},{"resource_id":"x"}]`))
	})
	ctx := context.Background()
	records, err := s.ListPrivateTrackers(ctx, privateTrackersTestUID)
	if err != nil || len(records) != 2 {
		t.Fatalf("ListPrivateTrackers = %+v, %v; want 2 records", records, err)
	}
	r := records[0]
	if r.ResourceID != cacheTestHash || strings.Join(r.Hosts, ",") != "tracker.example,b.invalid" || r.Source != "upload" ||
		!r.CreatedAt.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) ||
		!r.UpdatedAt.Equal(time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("record = %+v", r)
	}
	n, err := s.DeletePrivateTrackers(ctx, privateTrackersTestUID)
	if err != nil || n != 3 {
		t.Errorf("DeletePrivateTrackers = %d, %v; want 3", n, err)
	}
	_, _ = s.ListPrivateTrackers(ctx, "a/b?c")
	want := []string{
		"GET /internal/private-trackers/" + privateTrackersTestUID + "?",
		"DELETE /internal/private-trackers/" + privateTrackersTestUID + "?",
		"GET /internal/private-trackers/a%2Fb%3Fc?",
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

// An old rest-api answers 404: that is no endpoint, not no records, so both
// calls fail closed. No error text carries the uid: the path holds it.
func TestPrivateTrackers404IsError(t *testing.T) {
	for _, c := range []struct {
		name string
		s    *Api
	}{
		{"404", answering(t, http.StatusNotFound, "404 page not found")},
		{"500", answering(t, http.StatusInternalServerError, `{"error":"boom"}`)},
		{"dropped", restAPIStub(t, func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })},
	} {
		ctx := context.Background()
		records, lerr := c.s.ListPrivateTrackers(ctx, privateTrackersTestUID)
		n, derr := c.s.DeletePrivateTrackers(ctx, privateTrackersTestUID)
		if lerr == nil || records != nil {
			t.Errorf("%s: ListPrivateTrackers = %+v, %v; want an error", c.name, records, lerr)
		}
		if derr == nil || n != 0 {
			t.Errorf("%s: DeletePrivateTrackers = %d, %v; want an error", c.name, n, derr)
		}
		for _, err := range []error{lerr, derr} {
			if err != nil && strings.Contains(err.Error(), privateTrackersTestUID) {
				t.Errorf("%s: error carries the uid: %v", c.name, err)
			}
		}
	}
}

// private_unavailable at any status is a store without private records:
// nothing to export or delete. A 403 without a code is still an error.
func TestPrivateTrackersUnavailableIsEmpty(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusForbidden} {
		s := answering(t, status, `{"error":"x","code":"private_unavailable"}`)
		records, lerr := s.ListPrivateTrackers(ctx, privateTrackersTestUID)
		n, derr := s.DeletePrivateTrackers(ctx, privateTrackersTestUID)
		if len(records) != 0 || lerr != nil || n != 0 || derr != nil {
			t.Errorf("%d: got %+v, %v and %d, %v; want nothing and no error", status, records, lerr, n, derr)
		}
	}
	s := answering(t, http.StatusForbidden, `{"error":"internal only"}`)
	if _, err := s.ListPrivateTrackers(ctx, privateTrackersTestUID); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListPrivateTrackers 403 without a code: %v, want ErrForbidden", err)
	}
	if _, err := s.DeletePrivateTrackers(ctx, privateTrackersTestUID); !errors.Is(err, ErrForbidden) {
		t.Errorf("DeletePrivateTrackers 403 without a code: %v, want ErrForbidden", err)
	}
}

// Through RapidAPI (host and key both set) the gateway has no /internal/ and
// one uid serves every web-ui user: nothing is asked, nothing is found.
func TestPrivateTrackersRapidAPISkips(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--rapidapi-host=gateway.invalid", "--rapidapi-key=0000"}); err != nil {
		t.Fatal(err)
	}
	if !New(cli.NewContext(cli.NewApp(), fs, nil), http.DefaultClient).rapidAPI {
		t.Error("New with RapidAPI host and key: rapidAPI = false")
	}

	var requests atomic.Int32
	s := restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	s.rapidAPI = true
	ctx := context.Background()
	records, lerr := s.ListPrivateTrackers(ctx, privateTrackersTestUID)
	n, derr := s.DeletePrivateTrackers(ctx, privateTrackersTestUID)
	if records != nil || lerr != nil || n != 0 || derr != nil {
		t.Errorf("got %+v, %v and %d, %v; want nil, nil and 0, nil", records, lerr, n, derr)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("rest-api got %d requests, want none", got)
	}
}
