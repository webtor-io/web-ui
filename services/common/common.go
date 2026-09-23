package common

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

// SHA1R is a sanity check that a resource id from the URL carries some hex
// (handlers/resource/get.go); it is not a parser. What a person pastes into
// the form goes through ResolveQueryHash, which is strict: a 5-hex run is how
// "S01E02" and every URL with a numeric id used to become a bogus infohash.
var SHA1R = regexp.MustCompile("(?i)[0-9a-f]{5,40}")

// V1HashTokenR matches a standalone 40-hex v1 infohash inside a longer string:
// the \b guards reject a run cut out of a longer hex string (a 64-hex v2
// digest must not become its first 40 characters).
var V1HashTokenR = regexp.MustCompile(`(?i)\b[0-9a-f]{40}\b`)

// magnetInTextR finds a magnet pasted with something around it
// ("url=magnet:?xt=…", a line copied from a forum); it runs to the first
// whitespace.
var magnetInTextR = regexp.MustCompile(`(?i)magnet:\?\S*`)

// What ResolveQueryHash can say about a query it cannot use. Each one has its
// own message: web.ClassifyError matches them with errors.Is, through every
// wrapper, so a title that happens to contain "Unavailable" is not read as a
// backend outage. ErrQueryFreeText keeps the wording the logs have always
// carried for this case.
var (
	ErrMagnetInvalid   = errors.New("failed to parse magnet")
	ErrMagnetNoHash    = errors.New("no infohash found in magnet")
	ErrV2Only          = errors.New("v2-only infohash (btmh) is not supported, a v1 btih infohash is required")
	ErrQueryWebPage    = errors.New("query is a link to a web page, not to a torrent")
	ErrQueryTorrentURL = errors.New("query is a link to a .torrent file, which is not fetched")
	ErrQueryFreeText   = errors.New("no infohash found in query")
	// ErrHashLength is what a *HashLengthError is (errors.Is); the error
	// itself carries the numbers the message quotes.
	ErrHashLength = errors.New("infohash of the wrong length")
)

// HashLengthError is a query made of nothing but infohash characters, too
// many of them to be a word or a number, but not as many as an infohash has:
// a character lost or doubled on the way to the clipboard. It is told so,
// with both numbers, instead of "Webtor does not search by title".
type HashLengthError struct {
	Len  int // characters in the query
	Full int // characters in a full infohash of that alphabet: 40 hex, 32 base32
}

func (e *HashLengthError) Error() string {
	return fmt.Sprintf("infohash has %d characters, a full one has %d", e.Len, e.Full)
}

func (e *HashLengthError) Is(target error) bool { return target == ErrHashLength }

// ResolveQueryHash resolves a user query to a lowercase v1 infohash plus a
// magnet URI safe to pass downstream. It accepts, after trimming:
//
//   - a magnet URI, also one inside other text ("url=magnet:?…"); one that
//     does not parse is tried once more percent-decoded (a magnet that was
//     itself a query value: "magnet:?xt%3Durn:btih:…%26dn%3D…"), then for a
//     standalone 40-hex token ("magnet://?xt=urn:btih:<hash>");
//   - the whole query being a v1 infohash: 40 hex or 32 base32, any case;
//   - a standalone 40-hex token anywhere else: in a URL (a resource page
//     link, a .torrent cache that names files by hash), with or without a
//     scheme, or next to a name ("Info Hash: <hash>", "<name> <hash>").
//
// Everything else is refused with an error that says what it was (see the
// Err* values above). Shorter hex runs are never taken out of a longer
// string: that is what turned "S01E02" into btih:01e02 and a dead-magnet card
// a minute later. V1HashTokenR's \b guards are what keep them out: a 40-hex
// token cannot be cut from "S01E02", a numeric id or a 64-hex v2 digest.
//
// Hybrid magnets may list urn:btmh before urn:btih, and magnet2torrent's
// parser takes the first xt, so the magnet is rebuilt with the v1 hash only.
func ResolveQueryHash(query string) (hash string, magnet string, err error) {
	query = strings.TrimSpace(query)
	if hasPrefixFold(query, "magnet:") {
		return resolveMagnetIn(query, query)
	}
	if m := magnetInTextR.FindString(query); m != "" {
		return resolveMagnetIn(m, query)
	}
	if h, ok := bareV1Hash(query); ok {
		return h, "magnet:?xt=urn:btih:" + h, nil
	}
	if isV2Digest(query) {
		return "", "", ErrV2Only
	}
	if err := hashLength(query); err != nil {
		return "", "", err
	}
	if h, ok := v1HashToken(query); ok {
		return h, "magnet:?xt=urn:btih:" + h, nil
	}
	if isWebURL(query) {
		if isTorrentFileURL(query) {
			return "", "", ErrQueryTorrentURL
		}
		return "", "", ErrQueryWebPage
	}
	return "", "", ErrQueryFreeText
}

// resolveMagnetIn resolves magnet m, found in query. A magnet that does not
// parse is not given up on while the person's intent is still readable:
//
//   - percent-decoded once, it may parse: "magnet:?xt%3Durn:btih:…%26dn%3D…"
//     is a magnet that was a query value (/magnet2torrent?magnet=…) and kept
//     its encoding: 1–4 such GETs a day in the six days to 2026-09-23, all
//     accepted by the old first-hex-run rule;
//   - a standalone 40-hex token in the query is the hash the person meant:
//     "magnet://?xt=urn:btih:<hash>", a word joiner pasted after the hash.
//
// Only then the magnet's own error. A v2-only magnet is not second-guessed:
// its answer is that v2 is not supported, not some other hash in the text.
func resolveMagnetIn(m, query string) (hash string, magnet string, err error) {
	hash, magnet, err = resolveMagnet(m)
	if err == nil || errors.Is(err, ErrV2Only) {
		return hash, magnet, err
	}
	if u, uerr := url.QueryUnescape(m); uerr == nil && u != m {
		if h, mg, derr := resolveMagnet(u); derr == nil {
			return h, mg, nil
		}
	}
	if h, ok := v1HashToken(query); ok {
		return h, "magnet:?xt=urn:btih:" + h, nil
	}
	return "", "", err
}

func v1HashToken(s string) (string, bool) {
	h := V1HashTokenR.FindString(s)
	return strings.ToLower(h), h != ""
}

func resolveMagnet(query string) (hash string, magnet string, err error) {
	// The /magnet route reassembles the URI as path + RawQuery, losing "?"
	query = "magnet:?" + strings.TrimPrefix(query[len("magnet:"):], "?")
	m, err := metainfo.ParseMagnetV2Uri(upperBase32Btih(query))
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrMagnetInvalid, err)
	}
	if !m.InfoHash.Ok {
		if m.V2InfoHash.Ok {
			return "", "", ErrV2Only
		}
		return "", "", ErrMagnetNoHash
	}
	m.V2InfoHash = g.Option[infohash_v2.T]{}
	return m.InfoHash.Value.HexString(), m.String(), nil
}

// upperBase32Btih upper-cases a 32-character (base32) btih. The library
// decodes it with base32.StdEncoding, which knows only the upper-case
// alphabet, and some sites print it in lower case: on 2026-09-23, 17 such
// magnets (35 submits) were refused as broken although they were not.
func upperBase32Btih(query string) string {
	u, err := url.Parse(query)
	if err != nil {
		return query
	}
	q := u.Query()
	changed := false
	for i, xt := range q["xt"] {
		if h, ok := strings.CutPrefix(xt, "urn:btih:"); ok && len(h) == 32 && h != strings.ToUpper(h) {
			q["xt"][i] = "urn:btih:" + strings.ToUpper(h)
			changed = true
		}
	}
	if !changed {
		return query
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// bareV1Hash reports whether the whole string is a v1 infohash — 40 hex or
// 32 base32, any case, optionally as a bare "urn:btih:" value — and returns
// it as lowercase hex.
func bareV1Hash(s string) (string, bool) {
	s = trimPrefixFold(s, "urn:btih:")
	switch len(s) {
	case 40:
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s), true
		}
	case 32:
		if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(s)); err == nil && len(b) == 20 {
			return hex.EncodeToString(b), true
		}
	}
	return "", false
}

// hashLength returns a *HashLengthError when the whole string (optionally a
// "urn:btih:" value) looks like an infohash of the wrong length, nil
// otherwise. "Looks like" is kept narrow, because the alternative answer,
// error.free_text, is the right one for everything else:
//
//   - hex: at least 16 characters, with both a digit and a letter. No word
//     is 16 hex letters long ("defaced" is 7) and a long plain number is
//     not a hash; a real hash one character short lacks a letter about
//     once in 10^8.
//   - base32: one case throughout, 24 to 40 characters, at least two of
//     the digits 2–7 and a letter. Titles run together ("SPIDERMAN2…")
//     have spaces, mixed case, a 0, 1, 8 or 9, or fewer digits.
//
// A 40-hex or 32-base32 string never gets here (bareV1Hash took it), nor a
// 64/68-hex v2 digest (isV2Digest).
func hashLength(s string) error {
	s = trimPrefixFold(s, "urn:btih:")
	if len(s) >= 16 && hexHashLikeR.MatchString(s) && digitR.MatchString(s) && hexLetterR.MatchString(s) {
		return &HashLengthError{Len: len(s), Full: 40}
	}
	if len(s) >= 24 && len(s) <= 40 && len(s) != 32 &&
		(base32UpperR.MatchString(s) || base32LowerR.MatchString(s)) &&
		len(base32DigitR.FindAllString(s, 2)) == 2 && letterR.MatchString(s) {
		return &HashLengthError{Len: len(s), Full: 32}
	}
	return nil
}

var (
	hexHashLikeR = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	digitR       = regexp.MustCompile(`[0-9]`)
	hexLetterR   = regexp.MustCompile(`[a-fA-F]`)
	base32UpperR = regexp.MustCompile(`^[A-Z2-7]+$`)
	base32LowerR = regexp.MustCompile(`^[a-z2-7]+$`)
	base32DigitR = regexp.MustCompile(`[2-7]`)
	letterR      = regexp.MustCompile(`[a-zA-Z]`)
)

// isV2Digest reports whether the whole string is a v2 (SHA-256) infohash:
// 64 hex, or the 68-hex multihash a btmh carries (1220 + digest).
func isV2Digest(s string) bool {
	s = trimPrefixFold(s, "urn:btmh:")
	if len(s) == 68 && strings.HasPrefix(s, "1220") {
		s = s[4:]
	}
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func isWebURL(s string) bool {
	return hasPrefixFold(s, "http://") || hasPrefixFold(s, "https://") || hasPrefixFold(s, "www.")
}

// isTorrentFileURL reports whether a web URL points at a .torrent file. The
// form does not fetch it (the embed does, from its own settings); the person
// is told to download the file and upload it.
func isTorrentFileURL(s string) bool {
	if hasPrefixFold(s, "www.") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".torrent")
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func trimPrefixFold(s, prefix string) string {
	if hasPrefixFold(s, prefix) {
		return s[len(prefix):]
	}
	return s
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
			Name: SessionSecretFlag,
			// No default. This one value is the HMAC key for the session
			// cookie store, the CSRF token, the Stremio playback JWT, the
			// unsubscribe JWT and — through the documented fallback in
			// services/s3 — the derivation of every user's S3 secret access
			// key. A working default meant an instance that never set it
			// came up healthy, silently, on a key published in this
			// repository: sessions forgeable for any user, and every user's
			// S3 secret computable from their (non-secret) access key id.
			//
			// Absent is now refused at startup rather than substituted. See
			// requireSessionSecret in serve.go.
			Usage:  "session secret (required; no default — see README)",
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
