package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testApi is an Api pointed at an httptest server: proxyURL passes the URL
// through untouched while useInternalTorrentHTTPProxy is false, so the
// request goes straight to srv.URL.
func testApi() *Api {
	return &Api{cl: http.DefaultClient}
}

// TestGetOpenSubtitlesChecksTheStatusFirst: a non-200 is reported with the
// status. Before this the body was decoded regardless, so a 429, a 502 or
// an HTML error page reached the job log as "unexpected end of JSON input"
// / "invalid character '<'" -- naming neither the service that refused nor
// the reason, on a page whose subtitles were simply missing.
func TestGetOpenSubtitlesChecksTheStatusFirst(t *testing.T) {
	for _, c := range []struct {
		name       string
		status     int
		retryAfter string
		body       string
	}{
		{name: "rate limited with a back-off", status: http.StatusTooManyRequests, retryAfter: "120"},
		{name: "empty 502", status: http.StatusBadGateway},
		{name: "html error page", status: http.StatusServiceUnavailable, body: "<html>nope</html>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.retryAfter != "" {
					w.Header().Set("Retry-After", c.retryAfter)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			subs, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json")
			if subs != nil {
				t.Errorf("tracks from a %d: %+v", c.status, subs)
			}
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err=%v, want a *StatusError", err)
			}
			if se.Status != c.status {
				t.Errorf("status=%d want %d", se.Status, c.status)
			}
			if se.RetryAfter != c.retryAfter {
				t.Errorf("retry-after=%q want %q", se.RetryAfter, c.retryAfter)
			}
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("the message must name the status, got %q", err.Error())
			}
			// The decoder's verdict must not be what the caller sees.
			if strings.Contains(err.Error(), "JSON") || strings.Contains(err.Error(), "json") {
				t.Errorf("a status error must not read as a parse failure: %q", err.Error())
			}
		})
	}
}

// TestGetOpenSubtitlesRetryAfterIsReportedNotObeyed: one request, whatever
// the header asks for. This call sits inside a 30 s job step with a viewer
// waiting on it; sleeping out somebody else's back-off would spend the
// whole budget and still answer nothing.
func TestGetOpenSubtitlesRetryAfterIsReportedNotObeyed(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json")
	if err == nil {
		t.Fatal("want an error")
	}
	if calls != 1 {
		t.Fatalf("requests=%d, want exactly one: Retry-After is reported, never slept on", calls)
	}
	if !strings.Contains(err.Error(), "1") {
		t.Errorf("the back-off must survive into the message, got %q", err.Error())
	}
}

// TestGetOpenSubtitlesEmptyBodyIsAnEmptyList: a 200 with nothing in it is
// "no subtitles for this file", an ordinary answer. json.Unmarshal rejects
// it ("unexpected end of JSON input"), which is exactly the message this
// task set out to stop showing.
func TestGetOpenSubtitlesEmptyBodyIsAnEmptyList(t *testing.T) {
	for _, body := range []string{"", "  \n"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		subs, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json")
		srv.Close()
		if err != nil {
			t.Fatalf("body=%q: err=%v", body, err)
		}
		if len(subs) != 0 {
			t.Fatalf("body=%q: tracks=%+v", body, subs)
		}
	}
}

// TestGetOpenSubtitlesDecodesA200: the ordinary path is unchanged, and a
// body that is neither empty nor valid JSON is still a parse error.
func TestGetOpenSubtitlesDecodesA200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"7","srclang":"en","label":"English","src":"sub.vtt","source":"hash"}]`))
	}))
	defer srv.Close()

	subs, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(subs) != 1 || subs[0].ID != "7" || subs[0].Source != "hash" || subs[0].SrcLang != "en" {
		t.Fatalf("tracks=%+v", subs)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"not":"a list"}`))
	}))
	defer bad.Close()
	if _, err := testApi().GetOpenSubtitles(context.Background(), bad.URL+"/s.json"); err == nil {
		t.Fatal("a 200 carrying the wrong shape is still an error")
	}
}

// TestGetOpenSubtitlesNotReadyIsNotAnEmptyList: video-info answers 200 with
// an empty list and a Retry-After while one of its search legs is still
// waiting on the seeder. Read as "this file has no subtitles" that answer is
// worse than the 404 it replaced: the stream job's rendered result is cached
// for ten minutes, so one early answer takes OpenSubtitles away from every
// viewer of the file for the rest of the bucket, with the step marked done.
func TestGetOpenSubtitlesNotReadyIsNotAnEmptyList(t *testing.T) {
	for _, c := range []struct {
		header string
		want   time.Duration
		body   string
	}{
		{header: "5", want: 5 * time.Second, body: "[]"},
		{header: "2", want: 2 * time.Second, body: ""},
		// Anything but delta-seconds is "not ready, no usable hint": an
		// HTTP-date is legal and unused here, and a skewed clock is a
		// worse number than none.
		{header: "Wed, 21 Oct 2026 07:28:00 GMT", want: 0, body: "[]"},
		{header: "soon", want: 0, body: "[]"},
	} {
		t.Run(c.header, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", c.header)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			subs, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json")
			if subs != nil {
				t.Errorf("tracks=%+v", subs)
			}
			if !errors.Is(err, ErrSubtitlesNotReady) {
				t.Fatalf("err=%v, want the not-ready sentinel", err)
			}
			var nr *SubtitlesNotReadyError
			if !errors.As(err, &nr) {
				t.Fatalf("err=%v, want *SubtitlesNotReadyError", err)
			}
			if nr.RetryAfter != c.want {
				t.Errorf("retry-after=%v want %v", nr.RetryAfter, c.want)
			}
		})
	}

	// Negative control for the rule's other half: a 200 with no Retry-After
	// is still an ordinary empty list, not a not-ready answer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if _, err := testApi().GetOpenSubtitles(context.Background(), srv.URL+"/s.json"); err != nil {
		t.Fatalf("an empty list without a back-off is an answer: %v", err)
	}
}
