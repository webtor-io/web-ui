package scripts

import (
	"flag"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

// The rule applyGraceRules puts on the viewer's primary claims carries a
// grace token bound to the torrent and to the viewer's session and domain:
// thp limits grace segments per (session, rate) and counts them in the
// viewer's /session-stats (docs/grace_token.md "Session").
func TestApplyGraceRules_TokenCarriesTheSession(t *testing.T) {
	const secret = "grace-test-secret"
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-secret=" + secret}); err != nil {
		t.Fatal(err)
	}
	s := &ActionScript{
		api:   api.New(cli.NewContext(cli.NewApp(), fs, nil), http.DefaultClient),
		grace: GraceSettings{Enabled: true, DurationSec: 1200, Rate: "50M"},
	}
	const hash = "1e1fdcd4cb67e6def1d27f7e199c1a62c2760929"
	exp := jwt.NewNumericDate(time.Now().Add(24 * time.Hour).Truncate(time.Second))
	c := &web.Context{ApiClaims: &api.Claims{SessionID: "8b6dceb4a6aa", Domain: "webtor.io", Rate: "5M",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: exp}}}
	sc := &StreamContent{}
	s.applyGraceRules(sc, hash, c)

	if len(c.ApiClaims.Rules) != 1 || c.ApiClaims.Hash != hash || sc.GraceDurationSec != 1200 {
		t.Fatalf("rules %+v, hash %q, duration %d", c.ApiClaims.Rules, c.ApiClaims.Hash, sc.GraceDurationSec)
	}
	p, err := jwt.Parse(c.ApiClaims.Rules[0].Token, func(*jwt.Token) (interface{}, error) { return []byte(secret), nil })
	if err != nil || !p.Valid {
		t.Fatalf("grace token: %v", err)
	}
	g := p.Claims.(jwt.MapClaims)
	want := map[string]string{"sessionID": "8b6dceb4a6aa", "domain": "webtor.io", "hash": hash, "rate": "50M", "kind": "grace", "role": "grace"}
	for k, v := range want {
		if g[k] != v {
			t.Errorf("grace token %s = %v, want %q", k, g[k], v)
		}
	}
	// It expires with the viewer's primary token, not never.
	if ge, err := g.GetExpirationTime(); err != nil || ge == nil || !ge.Equal(exp.Time) {
		t.Errorf("grace token exp = %v (%v), want the primary's %v", ge, err, exp)
	}
}
