// Package uploads serves an account's own uploads: their IDs, the quota and
// the write tokens.
package uploads

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

const (
	// Prefix starts every upload ID. An ID is the prefix, a kind (one hex
	// digit) and 27 hex, all lower case.
	Prefix = models.UploadPrefix
	// TokenTTL is how long a write token lives.
	TokenTTL = 15 * time.Minute
	// URLFlag is where the browser sends an upload's files.
	URLFlag = "upload-url"
)

// ErrFrozen refuses a token to an upload saved to Vault: the seeder no
// longer takes its writes.
var ErrFrozen = errors.New("upload is saved to Vault")

// HomeHosted answers: does the ID's content live on its home node rather than
// in a swarm? Any kind. Strict: 40 characters, lower-case hex only, the prefix.
func HomeHosted(id string) bool {
	if len(id) != 40 || !strings.HasPrefix(id, Prefix) {
		return false
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Owned answers: does the ID have one owner, the only one who writes it and
// the only one who sees it? Kind 0, a user's upload.
func Owned(id string) bool {
	return HomeHosted(id) && id[12] == '0'
}

// NewID is a new upload ID of kind 0.
func NewID() (string, error) {
	b := make([]byte, 14)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "failed to make an upload ID")
	}
	return Prefix + "0" + hex.EncodeToString(b)[:27], nil
}

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.StringFlag{
		Name:   URLFlag,
		Usage:  "torrent-http-proxy address the browser sends uploads to; empty turns uploads off",
		EnvVar: "UPLOAD_URL",
	})
}

// reason is why uploads are off, empty when they are on. Without a secret
// web-ui signs no write token, and through RapidAPI rest-api answers one
// account for every user, so it cannot keep an upload to its owner.
func reason(c *cli.Context) string {
	switch {
	case c.String(URLFlag) == "":
		return "UPLOAD_URL is not set"
	case !api.SecretConfigured(c):
		return "WEBTOR_API_SECRET is not set"
	case api.RapidAPIConfigured(c):
		return "private resources are not available through RapidAPI"
	}
	return ""
}

// Enabled says this instance takes uploads.
func Enabled(c *cli.Context) bool {
	return reason(c) == ""
}

// New is the uploads service, nil when uploads are off. It says at start
// which, and why off.
func New(c *cli.Context, pg *cs.PG, a *api.Api) *Service {
	if r := reason(c); r != "" {
		log.Info("uploads: off, " + r)
		return nil
	}
	log.Info("uploads: on")
	return &Service{pg: pg, api: a, url: c.String(URLFlag), key: api.Key(c), cl: http.DefaultClient}
}

// Grant is a write token and what the browser needs with it.
type Grant struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Token  string `json:"token"`
	APIKey string `json:"apiKey"`
	Cap    int64  `json:"cap"`
	Used   int64  `json:"used"`
}

// Grant signs a write token for id: the viewer's claims ac, bound to id,
// with up ("new" or "add") and the write limit lim, until exp. It drops what
// the viewer's claims carry for other uses: rules, the internal mark and
// their registered claims.
func (s *Service) Grant(ac *api.Claims, id, up string, lim, cap, used int64, exp time.Time) (*Grant, error) {
	c := *ac
	c.Hash, c.Rules, c.Internal, c.Up, c.Lim = id, nil, false, up, lim
	c.RegisteredClaims = jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)}
	token, err := s.api.SignClaims(&c)
	if err != nil {
		return nil, errors.Wrap(err, "failed to sign an upload token")
	}
	return &Grant{ID: id, URL: s.url + "/" + id, Token: token, APIKey: s.key, Cap: cap, Used: used}, nil
}
