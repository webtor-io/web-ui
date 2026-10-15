package scripts

import (
	"context"
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

// A hash torrent-store refuses (rest-api's 403) is not a dead magnet: on the
// lookup by hash or on the store of the magnet it ends as error.forbidden,
// never as the "nobody shares it, try ten minutes" card. A magnet that did
// not resolve and a broken link are still the card.
func TestStoreQueryRefusal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name      string
		get, post int    // rest-api's answers; a zero post is never asked
		body      string // the POST's error body
		key       string // the plain error's ClassifyError
		kind      string // the card's MagnetError.Kind
	}{
		{name: "lookup refused", get: http.StatusForbidden, key: "error.forbidden"},
		{name: "store refused", get: http.StatusNotFound, post: http.StatusForbidden, body: `{"error":"forbidden"}`, key: "error.forbidden"},
		{name: "nobody shares it", get: http.StatusNotFound, post: http.StatusInternalServerError, body: `{"error":"magnet timeout"}`, kind: "dead"},
		{name: "broken link", get: http.StatusNotFound, post: http.StatusBadRequest, body: `{"error":"failed to parse magnet: placeholder"}`, kind: "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/resource/"+hash:
					w.WriteHeader(tc.get)
				case r.Method == http.MethodPost && r.URL.Path == "/resource" && tc.post != 0:
					w.WriteHeader(tc.post)
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			for _, f := range api.RegisterFlags(nil) {
				f.Apply(fs)
			}
			if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
				"--webtor-secret=test", "--rapidapi-host="}); err != nil {
				t.Fatal(err)
			}
			s := &LoadScript{
				api:  api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client()),
				i18n: i18n.New(os.DirFS("../../locales")),
				c:    &web.Context{Lang: "en", ApiClaims: &api.Claims{}},
				args: &LoadArgs{},
			}
			j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)

			_, err := s.storeQuery(context.Background(), j, "magnet:?xt=urn:btih:"+hash)
			// The assertion jobs/load.go makes to pick the card.
			me, card := err.(*MagnetError)
			if tc.kind != "" {
				if !card || me.Kind != tc.kind {
					t.Fatalf("err=%v, want the %s card", err, tc.kind)
				}
				return
			}
			if err == nil || card {
				t.Fatalf("err=%v, want a plain error, not the card", err)
			}
			if got := web.ClassifyError(err); got != tc.key {
				t.Fatalf("ClassifyError(%q)=%s, want %s", err, got, tc.key)
			}
		})
	}
}
