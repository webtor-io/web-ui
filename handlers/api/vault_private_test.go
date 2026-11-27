package api

import (
	"net/http"
	"testing"

	"github.com/pkg/errors"

	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/vault"
)

// A refused private pledge answers with a code a client acts on: 422 for no
// record of its own (upload the .torrent first); rest-api's 403
// private_unavailable (a deployment that does not serve private torrents)
// passes as it came.
func TestPledgeError(t *testing.T) {
	for _, c := range []struct {
		err          error
		status       int
		code, reason string
	}{
		{errors.Wrap(vault.ErrPrivateNoRecord, "pledge refused"), http.StatusUnprocessableEntity, restapi.CodePrivateTorrent, restapi.ReasonNoRecord},
		{&restapi.PrivateError{Status: http.StatusForbidden, Code: restapi.CodePrivateUnavailable,
			Message: "private torrents are not available on this server"}, http.StatusForbidden, restapi.CodePrivateUnavailable, ""},
	} {
		e := pledgeError(c.err)
		if e.Status != c.status || e.Code != c.code || e.Reason != c.reason || e.Message == "" {
			t.Errorf("pledgeError(%v) = %d %q %q %q, want %d %q %q with a message", c.err, e.Status, e.Code, e.Reason, e.Message, c.status, c.code, c.reason)
		}
	}
}
