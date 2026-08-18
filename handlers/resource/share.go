package resource

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	sv "github.com/webtor-io/web-ui/services/common"
)

// bareInfohashR matches a standalone 40-hex v1 infohash token. Deliberately
// stricter than the search box's sv.SHA1R ({5,40}, first match): share-sheet
// input is arbitrary text and URLs, where a lenient match turns nearly any
// shared link into a bogus magnet ("facebook.com/story/123" → btih:faceb) and
// truncates v2-only btmh digests into syntactically-valid-but-nonexistent v1
// hashes. The \b guards also reject hex runs embedded in longer hex strings.
var bareInfohashR = regexp.MustCompile(`(?i)\b[0-9a-f]{40}\b`)

// share is the Web Share Target endpoint (manifest share_target): an
// installed PWA receives shared text from the Android share sheet here.
func (s *Handler) share(c *gin.Context) {
	path, ok := resolveSharePath(c.Query("title"), c.Query("text"), c.Query("url"))
	if !ok {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.Redirect(http.StatusFound, path)
}

// resolveSharePath extracts a magnet URI or bare infohash from shared
// share-sheet fields and maps it onto the existing GET /:resource_id
// magnet route. Returns false when nothing streamable was shared.
func resolveSharePath(title, text, url string) (string, bool) {
	candidates := []string{url, text, title}
	for _, cand := range candidates {
		if m, ok := extractMagnet(cand); ok {
			if _, canonical, err := sv.ResolveQueryHash(m); err == nil {
				return "/" + canonical, true
			}
		}
	}
	for _, cand := range candidates {
		if h := bareInfohashR.FindString(cand); h != "" {
			return "/magnet:?xt=urn:btih:" + strings.ToLower(h), true
		}
	}
	return "", false
}

// extractMagnet cuts a magnet URI out of surrounding text, up to the
// first whitespace character.
func extractMagnet(s string) (string, bool) {
	i := strings.Index(s, "magnet:?")
	if i < 0 {
		return "", false
	}
	m := s[i:]
	if j := strings.IndexFunc(m, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}); j >= 0 {
		m = m[:j]
	}
	return m, true
}
