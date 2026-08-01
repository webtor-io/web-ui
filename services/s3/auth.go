package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	signV4Algorithm = "AWS4-HMAC-SHA256"
	iso8601Format   = "20060102T150405Z"
	// maxSkew mirrors Amazon's own tolerance for header-signed requests.
	maxSkew = 15 * time.Minute
	// emptyPayloadHash is hex(sha256("")) — what SDKs sign for bodyless requests
	// when they omit the x-amz-content-sha256 header.
	emptyPayloadHash  = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	unsignedPayload   = "UNSIGNED-PAYLOAD"
	amzContentSHA256  = "X-Amz-Content-Sha256"
	amzDateHeader     = "X-Amz-Date"
	amzSignatureParam = "X-Amz-Signature"
)

// DeriveSecretKey computes the S3 secret access key for an access key id.
//
// Deliberately derived instead of stored: the access key id is the user's
// existing access token (models.AccessToken), so issuing, rotating and revoking
// S3 credentials all reuse the token flow untouched, and no migration or new
// user-keyed table is needed. The flip side — rotating the signing secret
// invalidates every user's S3 configuration at once — is documented in
// docs/s3.md.
func DeriveSecretKey(signingSecret string, accessKeyID string) string {
	m := hmac.New(sha256.New, []byte(signingSecret))
	m.Write([]byte("s3:" + accessKeyID))
	// URL-safe alphabet: the secret gets pasted into rclone.conf, YAML and shell
	// env, where '+' and '/' invite quoting accidents.
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// signature is a parsed SigV4 credential, from either the Authorization header
// or the presigned query string.
type signature struct {
	AccessKeyID   string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
	AmzDate       string
	Presigned     bool
	Expires       int64
}

func (s *signature) scope() string {
	return strings.Join([]string{s.Date, s.Region, s.Service, "aws4_request"}, "/")
}

// AccessKeyID returns the access key a request is signed with, without
// verifying anything. The early middleware uses it to feed the existing access
// token chain (see handlers/s3); the signature itself is checked later, in the
// handler, where the whole original request URI is still available.
func AccessKeyID(r *http.Request, query url.Values) string {
	sig, err := parseSignature(r, query)
	if err != nil {
		return ""
	}
	return sig.AccessKeyID
}

func parseSignature(r *http.Request, query url.Values) (*signature, *Error) {
	if cred := query.Get("X-Amz-Credential"); cred != "" {
		sig := &signature{Presigned: true}
		if err := sig.parseCredential(cred); err != nil {
			return nil, err
		}
		sig.SignedHeaders = splitSignedHeaders(query.Get("X-Amz-SignedHeaders"))
		sig.Signature = query.Get(amzSignatureParam)
		sig.AmzDate = query.Get(amzDateHeader)
		if e := query.Get("X-Amz-Expires"); e != "" {
			sig.Expires, _ = strconv.ParseInt(e, 10, 64)
		}
		if query.Get("X-Amz-Algorithm") != signV4Algorithm {
			return nil, newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Unsupported signature algorithm", nil)
		}
		if sig.Signature == "" {
			return nil, newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Missing signature", nil)
		}
		return sig, nil
	}

	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, newError(http.StatusForbidden, ErrCodeMissingSecurity, "Missing Authorization header", nil)
	}
	if !strings.HasPrefix(auth, signV4Algorithm+" ") {
		return nil, newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Only "+signV4Algorithm+" is supported", nil)
	}
	sig := &signature{
		AmzDate: r.Header.Get(amzDateHeader),
	}
	for _, part := range strings.Split(strings.TrimPrefix(auth, signV4Algorithm+" "), ",") {
		part = strings.TrimSpace(part)
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "Credential":
			if err := sig.parseCredential(v); err != nil {
				return nil, err
			}
		case "SignedHeaders":
			sig.SignedHeaders = splitSignedHeaders(v)
		case "Signature":
			sig.Signature = v
		}
	}
	if sig.AccessKeyID == "" || sig.Signature == "" || len(sig.SignedHeaders) == 0 {
		return nil, newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Malformed Authorization header", nil)
	}
	return sig, nil
}

func (s *signature) parseCredential(cred string) *Error {
	parts := strings.Split(cred, "/")
	if len(parts) != 5 {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Malformed credential scope", nil)
	}
	s.AccessKeyID, s.Date, s.Region, s.Service = parts[0], parts[1], parts[2], parts[3]
	if s.Service != "s3" {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Credential should be scoped to service s3", nil)
	}
	return nil
}

func splitSignedHeaders(s string) []string {
	if s == "" {
		return nil
	}
	hs := strings.Split(s, ";")
	for i, h := range hs {
		hs[i] = strings.ToLower(strings.TrimSpace(h))
	}
	sort.Strings(hs)
	return hs
}

// verifyRequest checks the SigV4 signature of r.
//
// uri MUST be the request URI as it arrived from the client: the S3 group runs
// behind middleware that rewrites URL.Path/RawQuery (the access-token chain), and
// signing over the rewritten value would fail every request. handlers/s3 keeps a
// parse of c.Request.RequestURI for exactly this reason.
func verifyRequest(r *http.Request, uri *url.URL, signingSecret string, now time.Time) (*signature, *Error) {
	query := uri.Query()
	sig, err := parseSignature(r, query)
	if err != nil {
		return nil, err
	}
	if err := sig.checkTime(now); err != nil {
		return nil, err
	}

	payloadHash := unsignedPayload
	if !sig.Presigned {
		payloadHash = r.Header.Get(amzContentSHA256)
		if payloadHash == "" {
			payloadHash = emptyPayloadHash
		}
	}

	canonicalQuery := canonicalQueryString(query, sig.Presigned)
	key := signingKey(DeriveSecretKey(signingSecret, sig.AccessKeyID), sig)

	var canonicalRequest, stringToSign string
	for _, override := range headerCandidates(sig.SignedHeaders) {
		headers, err := canonicalHeaders(r, sig.SignedHeaders, override)
		if err != nil {
			return nil, err
		}

		canonicalRequest = strings.Join([]string{
			r.Method,
			canonicalURI(uri),
			canonicalQuery,
			headers,
			strings.Join(sig.SignedHeaders, ";"),
			payloadHash,
		}, "\n")

		stringToSign = strings.Join([]string{
			signV4Algorithm,
			sig.AmzDate,
			sig.scope(),
			hashHex([]byte(canonicalRequest)),
		}, "\n")

		expected := hex.EncodeToString(sign(key, []byte(stringToSign)))
		if hmac.Equal([]byte(expected), []byte(sig.Signature)) {
			return sig, nil
		}
	}

	// Hand back what we computed, the way Amazon does. Without it the only way
	// to find out which component a proxy rewrote is to add logging and deploy.
	// Nothing here is secret: it is a restatement of the request the caller just
	// made, and it proves nothing about the key.
	e := newError(http.StatusForbidden, ErrCodeSignatureMismatch,
		"The request signature we calculated does not match the signature you provided",
		errors.Errorf("signature mismatch for key %s", sig.AccessKeyID))
	e.CanonicalRequest = canonicalRequest
	e.StringToSign = stringToSign
	return nil, e
}

// headerCandidates yields the header overrides to try, in order: first the
// request exactly as received, then values a CDN is known to substitute.
//
// aws-sdk-go-v2 — and therefore rclone — signs `accept-encoding`, and Cloudflare
// rewrites that header on its way to the origin. Nothing about the request the
// client signed survives that, so an untouched verification cannot succeed no
// matter how correct it is. Re-checking against the value the SDK actually sends
// costs one HMAC and gives up nothing: the signature still binds the method,
// path, query, host, date and payload hash, and it still cannot be produced
// without the secret.
//
// The durable fix is to stop a header-rewriting proxy from sitting in front of
// the S3 endpoint (see docs/s3.md); this keeps clients working until then, and
// covers whatever CDN ends up in front of us next.
func headerCandidates(signed []string) []map[string]string {
	candidates := []map[string]string{nil}
	for _, h := range signed {
		if h == "accept-encoding" {
			candidates = append(candidates,
				map[string]string{"accept-encoding": "identity"},
				map[string]string{"accept-encoding": ""},
			)
			break
		}
	}
	return candidates
}

func (s *signature) checkTime(now time.Time) *Error {
	if s.AmzDate == "" {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Missing "+amzDateHeader, nil)
	}
	t, err := time.Parse(iso8601Format, s.AmzDate)
	if err != nil {
		return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Malformed "+amzDateHeader, err)
	}
	if s.Presigned {
		if s.Expires <= 0 {
			return newError(http.StatusBadRequest, ErrCodeInvalidArgument, "Missing X-Amz-Expires", nil)
		}
		if now.After(t.Add(time.Duration(s.Expires) * time.Second)) {
			return newError(http.StatusForbidden, ErrCodeAccessDenied, "Request has expired", nil)
		}
		return nil
	}
	if d := now.Sub(t); d > maxSkew || d < -maxSkew {
		return newError(http.StatusForbidden, ErrCodeRequestTimeTooSkewed,
			"The difference between the request time and the current time is too large", nil)
	}
	return nil
}

// canonicalURI is the path exactly as the client encoded it. S3 (unlike every
// other AWS service) does not double-encode it.
func canonicalURI(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	return p
}

func canonicalQueryString(query url.Values, presigned bool) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		if presigned && k == amzSignatureParam {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		values := append([]string(nil), query[k]...)
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

func canonicalHeaders(r *http.Request, signed []string, override map[string]string) (string, *Error) {
	var b strings.Builder
	for _, name := range signed {
		var value string
		if v, ok := override[name]; ok {
			b.WriteString(name)
			b.WriteString(":")
			b.WriteString(trimAll(v))
			b.WriteString("\n")
			continue
		}
		switch name {
		case "host":
			value = r.Host
		case "content-length":
			value = strconv.FormatInt(r.ContentLength, 10)
			if v := r.Header.Get("Content-Length"); v != "" {
				value = v
			}
		default:
			vs, ok := r.Header[http.CanonicalHeaderKey(name)]
			if !ok {
				return "", newError(http.StatusBadRequest, ErrCodeInvalidArgument,
					"Signed header "+name+" is missing from the request", nil)
			}
			trimmed := make([]string, len(vs))
			for i, v := range vs {
				trimmed[i] = trimAll(v)
			}
			value = strings.Join(trimmed, ",")
		}
		b.WriteString(name)
		b.WriteString(":")
		b.WriteString(trimAll(value))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// trimAll strips surrounding whitespace and collapses internal runs of spaces,
// as required by the SigV4 canonical header rules.
func trimAll(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// uriEncode implements the AWS flavour of RFC 3986 percent-encoding.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteString("%")
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

func signingKey(secret string, sig *signature) []byte {
	k := sign([]byte("AWS4"+secret), []byte(sig.Date))
	k = sign(k, []byte(sig.Region))
	k = sign(k, []byte(sig.Service))
	return sign(k, []byte("aws4_request"))
}

func sign(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func hashHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
