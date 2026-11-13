package api

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

// restAPIStub is an Api built by New from flags, whose rest-api is h.
func restAPIStub(t *testing.T, h http.HandlerFunc) *Api {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
}

// claimsOf reads the claims of the X-Token restAPIStub's Api signed.
func claimsOf(t *testing.T, r *http.Request) jwt.MapClaims {
	t.Helper()
	c := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(r.Header.Get("X-Token"), c, func(*jwt.Token) (any, error) {
		return []byte("test"), nil
	}); err != nil {
		t.Errorf("X-Token: %v", err)
	}
	return c
}

// refusalsOf answers status, body and Retry-After: retry to a read and to a
// store of a resource, and returns what each call made of it.
func refusalsOf(t *testing.T, status int, body, retry string) map[string]error {
	t.Helper()
	s := restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
		if retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	ctx := context.Background()
	res, gerr := s.GetResource(ctx, &Claims{}, cacheTestHash)
	if res != nil {
		t.Errorf("GetResource %d %s: got %+v, want nothing", status, body, res)
	}
	stored, serr := s.StoreResource(ctx, &Claims{}, []byte("magnet:?xt=urn:btih:"+cacheTestHash))
	if stored != nil {
		t.Errorf("StoreResource %d %s: got %+v, want nothing", status, body, stored)
	}
	return map[string]error{"GetResource": gerr, "StoreResource": serr}
}

// Every code rest-api names becomes one *PrivateError carrying the status,
// code, reason, text and Retry-After as rest-api sent them, and a 429 that
// names no code is rate_limited. None reads as a ban or a bare private magnet.
func TestRestAPIPrivateCodes(t *testing.T) {
	for _, c := range []struct {
		status       int
		body, retry  string
		code, reason string
	}{
		{403, `{"error":"x","code":"private_torrent","reason":"sign_in"}`, "", "private_torrent", "sign_in"},
		{403, `{"error":"x","code":"private_torrent","reason":"no_record"}`, "", "private_torrent", "no_record"},
		{403, `{"error":"x","code":"private_torrent_file"}`, "", "private_torrent_file", ""},
		{403, `{"error":"x","code":"private_unavailable"}`, "", "private_unavailable", ""},
		{422, `{"error":"x","code":"private_needs_account"}`, "", "private_needs_account", ""},
		{422, `{"error":"x","code":"private_no_tracker"}`, "", "private_no_tracker", ""},
		{422, `{"error":"x","code":"private_too_many_trackers"}`, "", "private_too_many_trackers", ""},
		{422, `{"error":"x","code":"private_tracker_refused","reason":"key"}`, "", "private_tracker_refused", "key"},
		{422, `{"error":"x","code":"private_unavailable"}`, "", "private_unavailable", ""},
		{503, `{"error":"x","code":"private_verify_unavailable"}`, "5", "private_verify_unavailable", ""},
		{503, `{"error":"x","code":"store_outdated"}`, "", "store_outdated", ""},
		{429, `{"error":"x"}`, "60", "rate_limited", ""},
	} {
		for call, err := range refusalsOf(t, c.status, c.body, c.retry) {
			var pe *PrivateError
			if !errors.As(err, &pe) || pe.Status != c.status || pe.Code != c.code || pe.Reason != c.reason || pe.Message != "x" || pe.RetryAfter != c.retry {
				t.Errorf("%s %d %s: got %#v", call, c.status, c.code, err)
			}
			if errors.Is(err, ErrForbidden) || errors.Is(err, ErrPrivateMagnet) {
				t.Errorf("%s %s reads as a ban or a bare private magnet", call, c.code)
			}
		}
	}
}

// An answer without a code reads as before rest-api named codes: 403 is a
// ban, 422 a private magnet whatever its words, anything else a plain error.
// A body that does not parse names no code. A coded private_magnet is both a
// *PrivateError and ErrPrivateMagnet.
func TestRestAPIUncodedErrorsAsToday(t *testing.T) {
	for _, c := range []struct {
		status                   int
		body                     string
		forbidden, magnet, coded bool
	}{
		{403, `{"error":"forbidden: restricted by the rightholder"}`, true, false, false},
		{403, `{"error":"x","code":"private_torrent","reason":1}`, true, false, false}, // a broken body names no code
		{422, ``, false, true, false},
		{422, `{"error":"placeholder"}`, false, true, false},
		{422, `{"error":"x","code":"private_magnet"}`, false, true, true},
		{500, `{"error":"boom"}`, false, false, false},
	} {
		for call, err := range refusalsOf(t, c.status, c.body, "") {
			var pe *PrivateError
			if err == nil {
				t.Errorf("%s %d %s: no error", call, c.status, c.body)
				continue
			}
			if got := errors.Is(err, ErrForbidden); got != c.forbidden {
				t.Errorf("%s %d %s: errors.Is(%v, ErrForbidden) = %v, want %v", call, c.status, c.body, err, got, c.forbidden)
			}
			if got := errors.Is(err, ErrPrivateMagnet); got != c.magnet {
				t.Errorf("%s %d %s: errors.Is(%v, ErrPrivateMagnet) = %v, want %v", call, c.status, c.body, err, got, c.magnet)
			}
			if got := errors.As(err, &pe); got != c.coded {
				t.Errorf("%s %d %s: errors.As(%v, *PrivateError) = %v, want %v", call, c.status, c.body, err, got, c.coded)
			}
		}
	}
}

// GetTorrent looks at the status: a refusal is an error the minute cache does
// not keep, a 404 is no torrent and is not kept either, and only a 200's
// bytes are the torrent.
func TestGetTorrentRefusalIsError(t *testing.T) {
	torrent := []byte("d4:infod4:name1:aee")
	for _, c := range []struct {
		name     string
		status   int
		body     []byte
		code     string
		want     []byte
		requests int32
	}{
		{"refused", http.StatusForbidden, []byte(`{"error":"x","code":"private_torrent_file"}`), CodePrivateTorrentFile, nil, 2},
		{"not found", http.StatusNotFound, []byte(`{"error":"not found"}`), "", nil, 2},
		{"ok", http.StatusOK, torrent, "", torrent, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			var requests atomic.Int32
			s := restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/resource/"+cacheTestHash+".torrent" {
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write(c.body)
			})
			for i := 0; i < 2; i++ {
				data, err := s.GetTorrentCached(context.Background(), &Claims{}, cacheTestHash)
				var pe *PrivateError
				if c.code != "" {
					if !errors.As(err, &pe) || pe.Code != c.code {
						t.Errorf("call %d: err = %#v, want *PrivateError %s", i, err, c.code)
					}
				} else if err != nil {
					t.Errorf("call %d: err = %v, want none", i, err)
				}
				if !bytes.Equal(data, c.want) || (c.want == nil && data != nil) {
					t.Errorf("call %d: data = %q, want %q", i, data, c.want)
				}
			}
			if n := requests.Load(); n != c.requests {
				t.Errorf("requests = %d, want %d", n, c.requests)
			}
		})
	}
}

// A refusal is rest-api's 403 or 422 with a code, wrapped or not; a 503, a
// 429, a ban without a code and no error are not.
func TestIsPrivateRefusal(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"403", &PrivateError{Status: 403, Code: CodePrivateTorrent}, true},
		{"422", &PrivateError{Status: 422, Code: CodePrivateNoTracker}, true},
		{"wrapped 403", errors.Wrap(&PrivateError{Status: 403, Code: CodePrivateTorrent}, "failed"), true},
		{"503", &PrivateError{Status: 503, Code: CodePrivateVerifyUnavailable}, false},
		{"429", &PrivateError{Status: 429, Code: CodeRateLimited}, false},
		{"ban", ErrForbidden, false},
		{"nil", nil, false},
	} {
		if got := IsPrivateRefusal(c.err); got != c.want {
			t.Errorf("%s: IsPrivateRefusal = %v, want %v", c.name, got, c.want)
		}
	}
}
