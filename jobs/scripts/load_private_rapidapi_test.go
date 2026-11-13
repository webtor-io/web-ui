package scripts

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// serveTransport answers every request with h, whatever its host: the
// RapidAPI gateway's address is fixed at https://<host>:443.
type serveTransport struct{ h http.Handler }

func (t serveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h.ServeHTTP(w, r)
	return w.Result(), nil
}

// rapidAPIRun runs a site load of args for a signed-in user, with rest-api
// reached through RapidAPI and answered by h. It returns the resource id the
// run hands to the redirect (nil when there is none) and the run's error.
func rapidAPIRun(t *testing.T, h http.Handler, args *LoadArgs) (any, error) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--rapidapi-host=gateway.invalid", "--rapidapi-key=0000"}); err != nil {
		t.Fatal(err)
	}
	args.Source = "site"
	s := &LoadScript{
		api:  api.New(cli.NewContext(cli.NewApp(), fs, nil), &http.Client{Transport: serveTransport{h}}),
		i18n: i18n.New(os.DirFS("../../locales")),
		c:    &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"}},
		args: args,
	}
	j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
	err := s.Run(context.Background(), j)
	return j.Context.Value("respID"), err
}

// wantUnavailable says err is the private card the Jobs layer draws
// (renderPrivateError picks it by type) with error.private.unavailable and no
// way out offered, and that there is no resource to redirect to.
func wantUnavailable(t *testing.T, err error, respID any, status int) {
	t.Helper()
	var pe *api.PrivateError
	if !errors.As(err, &pe) || pe.Code != api.CodePrivateUnavailable || pe.Status != status {
		t.Fatalf("err=%v, want %d %s", err, status, api.CodePrivateUnavailable)
	}
	if got := web.ClassifyError(err); got != "error.private.unavailable" {
		t.Errorf("ClassifyError=%s, want error.private.unavailable", got)
	}
	if r := web.NewPrivateRefusal(pe, true); r.SignIn || r.Upload {
		t.Errorf("the card offers sign-in=%v upload=%v, which would not help here", r.SignIn, r.Upload)
	}
	if respID != nil {
		t.Errorf("respID=%v: the job would redirect to the torrent", respID)
	}
}

// Through RapidAPI one uid serves every user of the instance, so a private
// torrent's right would be everybody's (decision 42): a private .torrent is
// refused before it reaches rest-api, with the "not available on this
// server" card.
func TestRapidAPIModePrivateUploadRefused(t *testing.T) {
	torrent, _ := embedTestTorrent(t, true)
	asked := 0
	respID, err := rapidAPIRun(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.WriteHeader(http.StatusTeapot)
	}), &LoadArgs{File: torrent})
	if asked != 0 {
		t.Errorf("rest-api got %d requests, want none", asked)
	}
	wantUnavailable(t, err, respID, http.StatusUnprocessableEntity)
}

// A magnet's privacy is known only from rest-api's answer: a resource that
// comes back private gets the same card, not the redirect to its page.
func TestRapidAPIModePrivateMagnetResultRefused(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	posted := false
	respID, err := rapidAPIRun(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource/"+h:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			posted = true
			_, _ = w.Write([]byte(`{"id":"` + h + `","private":true}`))
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}), &LoadArgs{Query: "magnet:?xt=urn:btih:" + h + "&tr=" + url.QueryEscape("http://tracker.example/announce")})
	if !posted {
		t.Error("the magnet did not reach rest-api: only its answer says it is private")
	}
	wantUnavailable(t, err, respID, http.StatusUnprocessableEntity)
}

// A hash rest-api already knows as private. A bare magnet of one the
// instance's account can read loads as before, and its page refuses it
// (GetResource, handlers/api TestJSONAPIRapidAPIModePrivateReads); the run
// keeps the private flag, so posters still read it as no source. rest-api's
// private_torrent there names that one account, not the user: the card says
// "not available", not "sign in" or "upload your .torrent".
func TestRapidAPIModeKnownPrivate(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	lookup := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/resource/"+h {
				t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	t.Run("the account's record", func(t *testing.T) {
		respID, err := rapidAPIRun(t, lookup(http.StatusOK, `{"id":"`+h+`","private":true}`), &LoadArgs{Query: "magnet:?xt=urn:btih:" + h})
		if err != nil || respID != h {
			t.Errorf("err=%v respID=%v, want the hash", err, respID)
		}
	})
	t.Run("no record", func(t *testing.T) {
		respID, err := rapidAPIRun(t, lookup(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`),
			&LoadArgs{Query: "magnet:?xt=urn:btih:" + h})
		wantUnavailable(t, err, respID, http.StatusForbidden)
	})
}

// A public .torrent and a public magnet load through RapidAPI as before, a
// magnet with a tracker too: only the answer "private" is refused.
func TestRapidAPIModePublicUnchanged(t *testing.T) {
	torrent, th := embedTestTorrent(t, false)
	const mh = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		name string
		args *LoadArgs
		hash string
	}{
		{"public .torrent", &LoadArgs{File: torrent}, th},
		{"public magnet", &LoadArgs{Query: "magnet:?xt=urn:btih:" + mh + "&tr=" + url.QueryEscape("http://tracker.example/announce")}, mh},
	} {
		t.Run(c.name, func(t *testing.T) {
			posted := false
			respID, err := rapidAPIRun(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/resource/"+c.hash:
					w.WriteHeader(http.StatusNotFound)
				case r.Method == http.MethodPost && r.URL.Path == "/resource":
					posted = true
					_, _ = w.Write([]byte(`{"id":"` + c.hash + `","private":false}`))
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			}), c.args)
			if err != nil {
				t.Fatal(err)
			}
			if !posted || respID != c.hash {
				t.Errorf("posted=%v respID=%v, want the store and %s", posted, respID, c.hash)
			}
		})
	}
}
