package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// drain reads the stream until it closes, failing the test if it does not
// close within d — a stream that never closes is a leaked goroutine.
func drain(t *testing.T, ch <-chan SessionStatsData, d time.Duration) []SessionStatsData {
	t.Helper()
	var got []SessionStatsData
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("stream did not close within %v (got %d events)", d, len(got))
			return got
		}
	}
}

func sseServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, body)
	}))
}

// The contract's two shapes: a limiter present ("throttled" even when 0) and
// no limiter at all (the field absent) — they must stay distinguishable.
func TestSessionStats_ParsesEventsAndClosesOnEOF(t *testing.T) {
	srv := sseServer(
		"data: {\"window_sec\":5,\"bytes_per_sec\":625000.5,\"conns\":2,\"rate\":\"5M\",\"throttled\":0.8}\n\n" +
			"data: {\"window_sec\":5,\"bytes_per_sec\":0,\"conns\":0,\"rate\":\"5M\",\"throttled\":0}\n\n" +
			"data: {\"window_sec\":5,\"bytes_per_sec\":1200,\"conns\":1}\n\n")
	defer srv.Close()

	a := &Api{cl: srv.Client()}
	ch, err := a.SessionStats(context.Background(), srv.URL+"/session-stats/abc?token=t")
	if err != nil {
		t.Fatal(err)
	}
	got := drain(t, ch, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(got), got)
	}
	if e := got[0]; e.WindowSec != 5 || e.BytesPerSec != 625000.5 || e.Conns != 2 || e.Rate != "5M" || e.Throttled == nil || *e.Throttled != 0.8 {
		t.Errorf("first event parsed wrong: %+v", e)
	}
	if e := got[1]; e.Throttled == nil || *e.Throttled != 0 {
		t.Errorf("throttled 0 means a limiter that never waited, not an absent one: %+v", e)
	}
	if e := got[2]; e.Throttled != nil || e.Rate != "" {
		t.Errorf("no limiter must read as absent throttled: %+v", e)
	}
}

// "active" -- a request of the session for the torrent was open at some
// moment since thp's previous event -- has three answers: true, false, and
// not said (a thp from before the field), which the meter reads by the old
// rule. Absent must not read as false.
func TestSessionStats_ActiveIsKnownOnlyWhenSent(t *testing.T) {
	srv := sseServer(
		"data: {\"window_sec\":5,\"bytes_per_sec\":1150000,\"conns\":0,\"active\":true}\n\n" +
			"data: {\"window_sec\":5,\"bytes_per_sec\":900000,\"conns\":0,\"active\":false}\n\n" +
			"data: {\"window_sec\":5,\"bytes_per_sec\":900000,\"conns\":0}\n\n")
	defer srv.Close()

	a := &Api{cl: srv.Client()}
	ch, err := a.SessionStats(context.Background(), srv.URL+"/session-stats/abc?token=t")
	if err != nil {
		t.Fatal(err)
	}
	got := drain(t, ch, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(got), got)
	}
	if e := got[0]; e.Active == nil || !*e.Active || e.Conns != 0 {
		t.Errorf("active true: %+v", e)
	}
	if e := got[1]; e.Active == nil || *e.Active {
		t.Errorf("active false must be a known false: %+v", e)
	}
	if e := got[2]; e.Active != nil {
		t.Errorf("an old thp's event: active must read as not said, got %v", *e.Active)
	}
}

// Any non-200 is "unavailable" — an old thp without the route, a session
// without an ID (404), a refused token (403), an upstream failure — and the
// status survives so the caller can tell a final answer from a transient one.
func TestSessionStats_Non200IsAnError(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest, http.StatusBadGateway} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = fmt.Fprint(w, "data: {\"conns\":1}\n\n")
		}))
		a := &Api{cl: srv.Client()}
		ch, err := a.SessionStats(context.Background(), srv.URL+"/session-stats/abc")
		srv.Close()
		if err == nil || ch != nil {
			t.Errorf("%d: want an error and no stream, got ch=%v err=%v", code, ch, err)
			continue
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Status != code {
			t.Errorf("%d: the status must survive as *StatusError, got %v", code, err)
		}
	}
}

// A broken line is skipped, not fatal: the next second brings a good one.
func TestSessionStats_MalformedJSONIsSkipped(t *testing.T) {
	srv := sseServer(
		"data: {\"bytes_per_sec\": oops\n\n" +
			": comment\n\n" +
			"event: message\n" +
			"data: {\"window_sec\":5,\"bytes_per_sec\":10,\"conns\":1}\n\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.SessionStats(context.Background(), srv.URL+"/session-stats/abc")
	if err != nil {
		t.Fatal(err)
	}
	got := drain(t, ch, 2*time.Second)
	if len(got) != 1 || got[0].BytesPerSec != 10 {
		t.Errorf("want only the well-formed event, got %+v", got)
	}
}

// The buffer stays small: a line past the cap ends the stream instead of
// growing a buffer per viewer (see the ~300MB note on Stats).
func TestSessionStats_OversizedLineEndsTheStream(t *testing.T) {
	srv := sseServer("data: {\"pad\":\"" + strings.Repeat("x", sessionStatsMaxLine+1) + "\"}\n\n" +
		"data: {\"conns\":1}\n\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.SessionStats(context.Background(), srv.URL+"/session-stats/abc")
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch, 2*time.Second); len(got) != 0 {
		t.Errorf("nothing after an oversized line, got %+v", got)
	}
}

// The stream lives as long as the status stream that opened it: cancelling
// the context closes the channel (the reader goroutine is gone) and drops the
// connection to thp — whether the reader is blocked handing over an event
// nobody reads any more, or waiting for thp's next line.
func TestSessionStats_CancelClosesTheStream(t *testing.T) {
	for _, tc := range []struct {
		name  string
		every time.Duration // 0: one event, then silence
	}{
		{"blocked on send", 10 * time.Millisecond},
		{"blocked on read", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gone := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				write := func() {
					_, _ = fmt.Fprint(w, "data: {\"window_sec\":5,\"bytes_per_sec\":1,\"conns\":1}\n\n")
					fl.Flush()
				}
				write()
				var tick <-chan time.Time
				if tc.every > 0 {
					tk := time.NewTicker(tc.every)
					defer tk.Stop()
					tick = tk.C
				}
				for {
					select {
					case <-r.Context().Done():
						close(gone)
						return
					case <-tick:
						write()
					}
				}
			}))
			defer srv.Close()

			ctx, cancel := context.WithCancel(context.Background())
			a := &Api{cl: srv.Client()}
			base := runtime.NumGoroutine()
			ch, err := a.SessionStats(ctx, srv.URL+"/session-stats/abc")
			if err != nil {
				t.Fatal(err)
			}
			<-ch // one event, then stop reading — like a status loop that returned
			time.Sleep(50 * time.Millisecond)
			cancel()
			select {
			case <-gone:
			case <-time.After(2 * time.Second):
				t.Fatal("the connection to thp outlived the context")
			}
			// Nobody reads ch any more: the reader goroutine has to leave on
			// its own. Reading here would unblock a send and hide the leak.
			deadline := time.Now().Add(2 * time.Second)
			for runtime.NumGoroutine() > base {
				if time.Now().After(deadline) {
					t.Fatalf("reader goroutine leaked: %d > %d", runtime.NumGoroutine(), base)
				}
				time.Sleep(5 * time.Millisecond)
			}
			if _, ok := <-ch; ok {
				t.Error("the stream must be closed once its reader is gone")
			}
		})
	}
}

// Same request path as Stats: with the internal proxy on, the URL's host is
// swapped for the configured thp service — the one place that knows how to
// reach thp from here.
func TestSessionStats_GoesThroughTheProxyPath(t *testing.T) {
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = fmt.Fprint(w, "data: {\"conns\":1}\n\n")
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	a := &Api{cl: srv.Client(), useInternalTorrentHTTPProxy: true, torrentHTTPProxyHost: host, torrentHTTPProxyPort: p}
	ch, err := a.SessionStats(context.Background(), "https://node1.api.example.invalid/session-stats/abc?token=t&api-key=k")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch, 2*time.Second)
	if path != "/session-stats/abc" || query != "token=t&api-key=k" {
		t.Errorf("request reached thp as %q ? %q", path, query)
	}
}

// The URL carries a token minted for this stream; a transport error must not
// carry it into a log line.
func TestSessionStats_ErrorsDoNotQuoteTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL + "/session-stats/abc?api-key=k&token=SECRET-TOKEN"
	srv.Close() // connection refused
	a := &Api{cl: http.DefaultClient}
	_, err := a.SessionStats(context.Background(), u)
	if err == nil {
		t.Fatal("want an error from a closed server")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") || strings.Contains(err.Error(), "token=") {
		t.Errorf("error quotes the URL: %v", err)
	}
}

// The status derives the viewer's session stream from this export, so every
// URL in it must be on the standard domain: rest-api moves them there with
// use-premium-domain=false.
func TestExportResourceContentStandardDomain(t *testing.T) {
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = fmt.Fprint(w, `{"export_items":{}}`)
	}))
	defer srv.Close()
	a := &Api{cl: srv.Client(), url: srv.URL, prepareRequest: func(r *http.Request, _ *Claims) (*http.Request, error) { return r, nil }}
	if _, err := a.ExportResourceContentStandardDomain(context.Background(), &Claims{}, "abc", "root"); err != nil {
		t.Fatal(err)
	}
	if path != "/resource/abc/export/root" || query != "use-premium-domain=false" {
		t.Errorf("export asked as %q ? %q", path, query)
	}
	// The ordinary export keeps rest-api's default.
	if _, err := a.ExportResourceContent(context.Background(), &Claims{}, "abc", "root", ""); err != nil {
		t.Fatal(err)
	}
	if query != "" {
		t.Errorf("the ordinary export changed: %q", query)
	}
}
