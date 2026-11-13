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
// (renderPrivateError picks it by type) with error.private.unavailable, and
// that there is no resource to redirect to.
func wantUnavailable(t *testing.T, err error, respID any) {
	t.Helper()
	var pe *api.PrivateError
	if !errors.As(err, &pe) || pe.Code != api.CodePrivateUnavailable || pe.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err=%v, want 422 %s", err, api.CodePrivateUnavailable)
	}
	if got := web.ClassifyError(err); got != "error.private.unavailable" {
		t.Errorf("ClassifyError=%s, want error.private.unavailable", got)
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
	wantUnavailable(t, err, respID)
}

// A magnet's privacy is known only from rest-api's answer: a resource that
// comes back private gets the same card, not the redirect to its page.
func TestRapidAPIModePrivateMagnetResultRefused(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	respID, err := rapidAPIRun(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource/"+h:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			_, _ = w.Write([]byte(`{"id":"` + h + `","private":true}`))
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}), &LoadArgs{Query: "magnet:?xt=urn:btih:" + h + "&tr=" + url.QueryEscape("http://tracker.example/announce")})
	wantUnavailable(t, err, respID)
}

// A public .torrent and a public magnet load through RapidAPI as before.
func TestRapidAPIModePublicUnchanged(t *testing.T) {
	torrent, th := embedTestTorrent(t, false)
	const mh = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		name string
		args *LoadArgs
		hash string
	}{
		{"public .torrent", &LoadArgs{File: torrent}, th},
		{"public magnet", &LoadArgs{Query: "magnet:?xt=urn:btih:" + mh}, mh},
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
