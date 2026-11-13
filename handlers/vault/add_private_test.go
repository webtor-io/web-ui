package vault

import (
	"net/http"
	"testing"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

// A refused private pledge says why in the modal: no record of the user's
// own, or private pledges not open on this server yet.
func TestPledgeErrKeys(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{errors.Wrap(vault.ErrPrivateNoRecord, "pledge refused"), "error.vault_private_no_record"},
		{&api.PrivateError{Status: http.StatusUnprocessableEntity, Code: api.CodePrivateUnavailable}, "error.private.unavailable"},
	} {
		if got := web.ClassifyError(pledgeErr(c.err)); got != c.want {
			t.Errorf("ClassifyError(pledgeErr(%v)) = %q, want %q", c.err, got, c.want)
		}
	}
}
