package api

import (
	"net/http"
	"testing"

	"github.com/pkg/errors"

	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/vault"
)

// A refused private pledge answers 422 with a code a client acts on: no
// record of its own (upload the .torrent first), or private pledges not open
// on this server yet.
func TestPledgeError(t *testing.T) {
	for _, c := range []struct {
		err          error
		code, reason string
	}{
		{errors.Wrap(vault.ErrPrivateNoRecord, "pledge refused"), restapi.CodePrivateTorrent, restapi.ReasonNoRecord},
		{&restapi.PrivateError{Status: http.StatusUnprocessableEntity, Code: restapi.CodePrivateUnavailable,
			Message: "private torrents are not available for Vault on this server yet"}, restapi.CodePrivateUnavailable, ""},
	} {
		e := pledgeError(c.err)
		if e.Status != http.StatusUnprocessableEntity || e.Code != c.code || e.Reason != c.reason || e.Message == "" {
			t.Errorf("pledgeError(%v) = %d %q %q %q, want 422 %q %q with a message", c.err, e.Status, e.Code, e.Reason, e.Message, c.code, c.reason)
		}
	}
}
