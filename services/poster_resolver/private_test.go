package poster_resolver

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
)

// A poster is public and stays in the edge cache for a day: a private
// torrent's is the default one, never its frames or enrichment artwork, and
// a failed lookup is a 500, not the default. The service has no database,
// so reaching Resolve would fail with that instead.
func TestPosterOfPrivateTorrentIsDefault(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for name, c := range map[string]struct {
		answer   func(w http.ResponseWriter, internal bool)
		notFound bool
	}{
		"private": {answer: func(w http.ResponseWriter, internal bool) {
			if !internal {
				// What rest-api tells a caller without a record.
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent","reason":"sign_in"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"` + hash + `","private":true}`))
		}, notFound: true},
		"rest-api down": {answer: func(w http.ResponseWriter, _ bool) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"rest-api down"}`))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/resource/"+hash {
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				}
				p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
				c.answer(w, err == nil && p.Claims.(jwt.MapClaims)["internal"] == true)
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
			s := New(nil, &cs.PG{}, nil, nil, api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client()), "")

			_, err := s.Get(context.Background(), hash, "300.jpg", true, false)
			if c.notFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound (the handler's 404)", err)
				}
				return
			}
			if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "rest-api down") {
				t.Fatalf("err = %v, want rest-api's failure, not ErrNotFound", err)
			}
		})
	}
}
