package access_token

import "testing"

// TestTokenAuthPathAdmitsOnlyScopeCheckingSurfaces is the negative control for
// the gate that decides where an access token may establish identity.
//
// The middleware that resolves ?token= is global. Before this gate, a token
// minted for any purpose authenticated every route in the application, because
// scope is verified per-surface and only four surfaces verify it. The concrete
// consequence: a stremio:read token — a value that lives in a URL pasted into a
// third-party client, and which the S3 flow also publishes as an "access key
// id" — reached GET /profile/export, whose body contains every other access
// token of that account in cleartext, and POST /profile/delete.
//
// Negative control: make tokenAuthPath return true unconditionally and every
// "must not" case below fails.
func TestTokenAuthPathAdmitsOnlyScopeCheckingSurfaces(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		want bool
	}{
		// The four surfaces that check scope before acting.
		{name: "json api", path: "/api/v1/library", want: true},
		{name: "json api root", path: "/api", want: true},
		{name: "s3 protocol", path: "/s3/torrents/x.torrent", want: true},
		{name: "stremio addon", path: "/stremio/manifest.json", want: true},
		{name: "webdav filesystem", path: "/webdav/fs/", want: true},

		// The routes the missing gate exposed. These are the finding.
		{name: "gdpr export", path: "/profile/export", want: false},
		{name: "account deletion", path: "/profile/delete", want: false},
		{name: "profile page", path: "/profile", want: false},
		{name: "library ui", path: "/lib/movie", want: false},
		{name: "site root", path: "/", want: false},
		{name: "vault", path: "/vault/add", want: false},

		// Credential-issuing routes sit next to the token surfaces by name
		// and must stay out: they hand over the credentials themselves and
		// authenticate by session. A bare HasPrefix would have matched these.
		{name: "api credentials", path: "/api-credentials/key", want: false},
		{name: "s3 credentials", path: "/s3-credentials/generate", want: false},

		// Boundary cases for the same reason.
		{name: "prefix lookalike", path: "/apifoo", want: false},
		{name: "webdav lookalike", path: "/webdavfoo", want: false},
		{name: "stremio lookalike", path: "/stremiox/manifest.json", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenAuthPath(tt.path); got != tt.want {
				if tt.want {
					t.Errorf("tokenAuthPath(%q) = false; this surface authenticates by token and would stop working", tt.path)
				} else {
					t.Errorf("tokenAuthPath(%q) = true; a bearer token must not be an identity here", tt.path)
				}
			}
		})
	}
}
