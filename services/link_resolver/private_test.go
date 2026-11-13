package link_resolver

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
)

// restAPI is an *api.Api that talks to srv.
func restAPI(t *testing.T, srv *httptest.Server) *api.Api {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
}

// A stream names only an infohash. rest-api decides whether this user may
// play it before any debrid backend sees the hash: its refusal is the answer.
// The resolver has no database, so reaching the backend lookup would fail
// with that instead.
func TestResolveLinkAsksRestAPIFirst(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/resource/"+hash {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent","reason":"no_record"}`))
	}))
	t.Cleanup(srv.Close)
	s := New(srv.Client(), &cs.PG{}, restAPI(t, srv), nil)

	res, err := s.ResolveLink(context.Background(), uuid.NewV4(),
		&api.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"}, nil, hash, 0, true)
	var pe *api.PrivateError
	if !errors.As(err, &pe) || pe.Code != api.CodePrivateTorrent || pe.Reason != api.ReasonNoRecord {
		t.Fatalf("err = %v, want rest-api's private_torrent no_record", err)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil alongside the refusal", res)
	}
}
