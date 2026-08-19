package common

import (
	"net/url"
	"regexp"
	"strings"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

var SHA1R = regexp.MustCompile("(?i)[0-9a-f]{5,40}")

// ResolveQueryHash resolves a user query (magnet URI, bare infohash or text
// containing one) to a lowercase v1 infohash plus a magnet URI safe to pass
// downstream. Hybrid magnets may list urn:btmh before urn:btih, and both
// SHA1R first-match extraction and magnet2torrent's parser take the first xt,
// so the magnet is rebuilt with the v1 hash only.
func ResolveQueryHash(query string) (hash string, magnet string, err error) {
	if strings.HasPrefix(query, "magnet:") {
		// The /magnet route reassembles the URI as path + RawQuery, losing "?"
		if !strings.HasPrefix(query, "magnet:?") {
			query = "magnet:?" + strings.TrimPrefix(query, "magnet:")
		}
		m, err := metainfo.ParseMagnetV2Uri(query)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to parse magnet")
		}
		if !m.InfoHash.Ok {
			if m.V2InfoHash.Ok {
				return "", "", errors.New("v2-only (btmh) magnets are not supported, use a magnet with a btih infohash or upload the .torrent file")
			}
			return "", "", errors.New("no infohash found in magnet")
		}
		m.V2InfoHash = g.Option[infohash_v2.T]{}
		return m.InfoHash.Value.HexString(), m.String(), nil
	}
	h := SHA1R.Find([]byte(query))
	if h == nil {
		return "", "", errors.New("no infohash found in query")
	}
	hash = strings.ToLower(string(h))
	return hash, "magnet:?xt=urn:btih:" + hash, nil
}

var (
	DomainFlag        = "domain"
	DemoMagnetFlag    = "demo-magnet"
	DemoTorrentFlag   = "demo-torrent"
	SMTPHostFlag      = "smtp-host"
	SMTPUserFlag      = "smtp-user"
	SMTPPassFlag      = "smtp-pass"
	SMTPPortFlag      = "smtp-port"
	SMTPSecureFlag    = "smtp-secure"
	SMTPFromFlag      = "smtp-from"
	UseDirectLinks    = "use-direct-links"
	OnlyAuthorized    = "only-authorized"
	SessionSecretFlag = "secret"
	DisableWebDAVFlag = "disable-webdav"
	DisableS3Flag     = "disable-s3"
	S3SecretFlag      = "s3-signing-secret"
	S3DomainFlag      = "s3-domain"
	DisableAPIFlag    = "disable-api"
	APIDomainFlag     = "api-domain"
	DisableEmbedFlag  = "disable-embed"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	f = append(f,
		cli.StringFlag{
			Name:   DomainFlag,
			Usage:  "domain",
			Value:  "http://localhost:8080",
			EnvVar: "DOMAIN",
		},
		cli.StringFlag{
			Name:   DemoMagnetFlag,
			Usage:  "demo magnet",
			Value:  "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
			EnvVar: "DEMO_MAGNET",
		},
		cli.StringFlag{
			Name:   DemoTorrentFlag,
			Usage:  "demo torrent",
			Value:  "https://webtorrent.io/torrents/sintel.torrent",
			EnvVar: "DEMO_TORRENT",
		},
		cli.StringFlag{
			Name:   SMTPHostFlag,
			Usage:  "smtp host",
			EnvVar: "SMTP_HOST",
		},
		cli.StringFlag{
			Name:   SMTPUserFlag,
			Usage:  "smtp user",
			EnvVar: "SMTP_USER",
		},
		cli.StringFlag{
			Name:   SMTPPassFlag,
			Usage:  "smtp pass",
			EnvVar: "SMTP_PASS",
		},
		cli.IntFlag{
			Name:   SMTPPortFlag,
			Usage:  "smtp port",
			EnvVar: "SMTP_PORT",
			Value:  465,
		},
		cli.BoolTFlag{
			Name:   SMTPSecureFlag,
			Usage:  "smtp secure",
			EnvVar: "SMTP_SECURE",
		},
		cli.StringFlag{
			Name:   SMTPFromFlag,
			Usage:  "smtp from address (falls back to smtp user if empty)",
			EnvVar: "SMTP_FROM",
		},
		cli.BoolTFlag{
			Name:   UseDirectLinks,
			Usage:  "use direct links",
			EnvVar: "USE_DIRECT_LINKS",
		},
		cli.BoolFlag{
			// Off by default so webtor.io, which serves anonymous visitors,
			// is unaffected. Self-hosted turns it on: there a resource page is
			// reachable by anyone holding the hash, so an instance with an
			// administrator password would still serve its content to
			// strangers. Surfaces with their own authentication — the JSON
			// API, the Stremio addon, S3 — are exempt; see serve.go.
			Name:   OnlyAuthorized,
			Usage:  "require an authenticated user for every page of the web interface",
			EnvVar: "ONLY_AUTHORIZED",
		},
		cli.StringFlag{
			Name:   SessionSecretFlag,
			Usage:  "session secret",
			Value:  "secret123",
			EnvVar: "SESSION_SECRET",
		},
		cli.BoolFlag{
			Name:   DisableWebDAVFlag,
			Usage:  "disable webdav",
			EnvVar: "DISABLE_WEBDAV",
		},
		cli.BoolFlag{
			Name:   DisableS3Flag,
			Usage:  "disable s3",
			EnvVar: "DISABLE_S3",
		},
		cli.StringFlag{
			Name: S3DomainFlag,
			// Hostnames that serve the S3 API at their root, comma-separated.
			// A dedicated host is what lets clients use a bare endpoint, and it
			// is the hook for keeping a header-rewriting CDN out of the path
			// (see docs/s3.md). Empty means S3 is only reachable at DOMAIN/s3.
			Usage:  "hostnames serving the s3 api at the root (comma-separated)",
			EnvVar: "S3_DOMAIN",
		},
		cli.StringFlag{
			Name: S3SecretFlag,
			// The S3 secret access key is derived from this and the user's
			// access token (see services/s3.DeriveSecretKey) instead of being
			// stored, so rotating it invalidates every user's S3 config the
			// same way rotating SESSION_SECRET drops every session. Empty falls
			// back to the session secret.
			Usage:  "s3 signing secret (falls back to session secret)",
			EnvVar: "S3_SIGNING_SECRET",
		},
		cli.BoolFlag{
			Name:   DisableAPIFlag,
			Usage:  "disable json api",
			EnvVar: "DISABLE_API",
		},
		cli.StringFlag{
			Name: APIDomainFlag,
			// Hostnames that serve the JSON API at their root, comma-separated
			// (api.webtor.io). Requests to them are rewritten onto /api,
			// keeping the version in the path: api.webtor.io/v1/fs. Empty means
			// the API is only reachable at DOMAIN/api/v1.
			Usage:  "hostnames serving the json api at the root (comma-separated)",
			EnvVar: "API_DOMAIN",
		},
		cli.BoolFlag{
			Name:   DisableEmbedFlag,
			Usage:  "disable embed",
			EnvVar: "DISABLE_EMBED",
		},
	)

	return f
}

const AccessTokenParamName = "token"

func EscapePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
