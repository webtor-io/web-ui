package static

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// IndexNowKeyFlag names the IndexNow key of this host (docs/indexnow.md).
// A search engine accepts a URL submission for a host only if the host
// serves the submitted key at /<key>.txt, so the key file exists exactly
// when a key is configured: a deployment without one — any host but the
// one that registered it — serves no key and cannot be submitted for.
const IndexNowKeyFlag = "indexnow-key"

// indexNowKey is the format the IndexNow protocol allows for a key.
var indexNowKey = regexp.MustCompile(`^[a-zA-Z0-9-]{8,128}$`)

// registerIndexNowKey serves the key file for a configured key. A key the
// protocol would reject fails the start instead of serving a file no
// search engine accepts.
func registerIndexNowKey(r gin.IRouter, key string) error {
	if key == "" {
		return nil
	}
	if !indexNowKey.MatchString(key) {
		return errors.Errorf("--%s: an IndexNow key is 8 to 128 characters of a-z, A-Z, 0-9 and '-'", IndexNowKeyFlag)
	}
	r.GET("/"+key+".txt", func(c *gin.Context) {
		c.String(http.StatusOK, key)
	})
	return nil
}
