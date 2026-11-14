package enrich

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
)

// Enrichment reads the listing for the shared media-info row, whoever started
// it: as internal, never as the user who started the load or the library add.
func TestRetrieveTorrentItemsReadsAsInternal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	var claims jwt.MapClaims
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/resource/"+hash+"/list" {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
		if err != nil || !p.Valid {
			t.Errorf("X-Token did not validate: %v", err)
		} else {
			claims = p.Claims.(jwt.MapClaims)
		}
		_, _ = w.Write([]byte(`{"items":[],"items_count":0}`))
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
	e := &Enricher{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())}

	if _, err := e.retrieveTorrentItems(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	if claims["internal"] != true {
		t.Errorf("internal = %v, want true; claims %+v", claims["internal"], claims)
	}
	if _, hasUid := claims["uid"]; hasUid {
		t.Errorf("the listing was read as a user: %+v", claims)
	}
}
