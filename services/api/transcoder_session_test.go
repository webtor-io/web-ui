package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pkgerrors "github.com/pkg/errors"
)

// sessionServer answers POST /session the way it is told and records the
// request line it got.
func sessionServer(t *testing.T, status int, header http.Header, body string) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// Without a declaration the request is byte for byte what it was; with one,
// decode= is the last parameter and the query that was there (thp's token
// and api-key, in their order) is not re-encoded or sorted.
func TestCreateTranscoderSession_URL(t *testing.T) {
	const base = "/abc123/Film%20%231.mkv~hls?token=zzz&api-key=aaa"
	for _, c := range []struct {
		decode, want string
	}{
		{"", "/abc123/Film%20%231.mkv~hls/session?token=zzz&api-key=aaa"},
		{"hevc8,hevc10,hdr-pq", "/abc123/Film%20%231.mkv~hls/session?token=zzz&api-key=aaa&decode=hevc8%2Chevc10%2Chdr-pq"},
		{"unknown", "/abc123/Film%20%231.mkv~hls/session?token=zzz&api-key=aaa&decode=unknown"},
	} {
		srv, got := sessionServer(t, http.StatusOK, nil, `{"id":"s1","duration":12.5}`)
		a := &Api{cl: srv.Client()}
		if _, err := a.CreateTranscoderSession(context.Background(), srv.URL+base, c.decode); err != nil {
			t.Fatalf("%q: %v", c.decode, err)
		}
		if *got != c.want {
			t.Errorf("decode %q: requested %s, want %s", c.decode, *got, c.want)
		}
	}
	// A base without a query gets decode= as its only parameter.
	srv, got := sessionServer(t, http.StatusOK, nil, `{"id":"s1"}`)
	a := &Api{cl: srv.Client()}
	if _, err := a.CreateTranscoderSession(context.Background(), srv.URL+"/h/f.mkv~hls", "hevc8"); err != nil {
		t.Fatal(err)
	}
	if *got != "/h/f.mkv~hls/session?decode=hevc8" {
		t.Errorf("no query: %s", *got)
	}
}

// The route and its reason are read from the 200; a transcoder that
// predates routes leaves them empty.
func TestCreateTranscoderSession_Route(t *testing.T) {
	srv, _ := sessionServer(t, http.StatusOK, nil, `{"id":"s1","duration":7,"video_route":"passthrough","route_reason":"ok"}`)
	a := &Api{cl: srv.Client()}
	s, err := a.CreateTranscoderSession(context.Background(), srv.URL+"/h/f~hls", "hevc10")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "s1" || s.Duration != 7 || s.VideoRoute != "passthrough" || s.RouteReason != "ok" {
		t.Errorf("%+v", s)
	}
	srv, _ = sessionServer(t, http.StatusOK, nil, `{"id":"s2","duration":7}`)
	a = &Api{cl: srv.Client()}
	if s, err = a.CreateTranscoderSession(context.Background(), srv.URL+"/h/f~hls", ""); err != nil || s.VideoRoute != "" || s.RouteReason != "" {
		t.Errorf("old transcoder: %+v %v", s, err)
	}
}

// 415 and 503 are typed refusals carrying the route reason and Retry-After,
// with the text the error had before it was typed -- ClassifyError and the
// logs match on it -- and they are found through the wrappers the job adds.
// Any other failure is the untyped error it was.
func TestCreateTranscoderSession_Refusal(t *testing.T) {
	for _, c := range []struct {
		name       string
		status     int
		header     http.Header
		body       string
		reason     string
		retryAfter string
	}{
		{"old transcoder, 415", http.StatusUnsupportedMediaType, nil, "resolution over 1080p is not supported\n", "", ""},
		{"no declaration, 415", http.StatusUnsupportedMediaType, http.Header{"X-Video-Route-Reason": {"no_declaration"}}, "resolution over 1080p is not supported\n", "no_declaration", ""},
		{"needs_pq, 415", http.StatusUnsupportedMediaType, http.Header{"X-Video-Route-Reason": {"needs_pq"}}, "resolution over 1080p is not supported\n", "needs_pq", ""},
		{"probe failed, 503", http.StatusServiceUnavailable, http.Header{"X-Video-Route-Reason": {"probe_failed"}, "Retry-After": {"5"}}, "source check failed\n", "probe_failed", "5"},
		{"thp, 503 without a reason", http.StatusServiceUnavailable, nil, "", "", ""},
	} {
		srv, _ := sessionServer(t, c.status, c.header, c.body)
		a := &Api{cl: srv.Client()}
		_, err := a.CreateTranscoderSession(context.Background(), srv.URL+"/h/f~hls", "hevc8")
		wrapped := pkgerrors.Wrap(pkgerrors.Wrap(err, "failed to create transcoder session"), "failed to buffer session HLS")
		var tr *TranscoderRefusal
		if !errors.As(wrapped, &tr) {
			t.Fatalf("%s: not a refusal: %v", c.name, err)
		}
		if tr.Status != c.status || tr.Body != c.body || tr.Reason != c.reason || tr.RetryAfter != c.retryAfter || tr.Fallback {
			t.Errorf("%s: %+v", c.name, tr)
		}
		want := "transcoder session creation failed status=" + map[int]string{415: "415", 503: "503"}[c.status] + " body=" + c.body
		if err.Error() != want {
			t.Errorf("%s: Error() %q, want %q", c.name, err.Error(), want)
		}
	}
	srv, _ := sessionServer(t, http.StatusInternalServerError, nil, "internal error\n")
	a := &Api{cl: srv.Client()}
	_, err := a.CreateTranscoderSession(context.Background(), srv.URL+"/h/f~hls", "")
	var tr *TranscoderRefusal
	if err == nil || errors.As(err, &tr) || err.Error() != "transcoder session creation failed status=500 body=internal error\n" {
		t.Errorf("500: %v", err)
	}
}
