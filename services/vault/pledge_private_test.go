package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/private_torrent"
)

// Every pledge asks rest-api first, with the user's claims: a private torrent
// the user holds no record for is not theirs to pay for, and every private
// resource pledged so far already has its vault.resource row, which
// GetOrCreateResource used to return without asking anything.

var pledgerClaims = &api.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"}

// answering is a rest-api that answers every request with status and body.
func answering(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// withResourceRow is a Vault whose rest-api is h, on a disposable Postgres
// that already holds the vault.resource row of privateHash.
func withResourceRow(t *testing.T, h http.HandlerFunc) *Vault {
	t.Helper()
	db := pgtest.Start(t)
	s := newTestVault(t, h)
	s.pg = pgtest.CS(t, db)
	if _, err := vaultModels.CreateResource(context.Background(), db, privateHash, 1, "n"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPledgeCheckRunsBeforeTheEarlyReturn(t *testing.T) {
	s := withResourceRow(t, answering(http.StatusForbidden,
		`{"error":"private torrent: only a signed-in user who added it with their own tracker can play it","code":"private_torrent","reason":"no_record"}`))

	res, err := s.GetOrCreateResource(context.Background(), pledgerClaims, privateHash)
	if !errors.Is(err, ErrPrivateNoRecord) {
		t.Fatalf("err = %v, want ErrPrivateNoRecord", err)
	}
	if res != nil {
		t.Errorf("got resource %v: a pledge would follow", res.ResourceID)
	}
}

func TestPrivatePledgeClosedBeforeEnforce(t *testing.T) {
	s := newTestVault(t, answering(http.StatusOK, fmt.Sprintf(`{"id":%q,"name":"n","size":1,"private":true}`, privateHash)))
	s.pg = &cs.PG{} // no database: the open path ends there
	t.Cleanup(func() { private_torrent.SetEnforce(false) })

	private_torrent.SetEnforce(false)
	_, err := s.GetOrCreateResource(context.Background(), pledgerClaims, privateHash)
	var pe *api.PrivateError
	if !errors.As(err, &pe) || pe.Status != http.StatusUnprocessableEntity || pe.Code != api.CodePrivateUnavailable {
		t.Fatalf("before enforce: err = %v, want *api.PrivateError 422 private_unavailable", err)
	}

	private_torrent.SetEnforce(true)
	_, err = s.GetOrCreateResource(context.Background(), pledgerClaims, privateHash)
	if errors.As(err, &pe) {
		t.Fatalf("enforced: err = %v, want the pledge to go on", err)
	}
	if err == nil || !strings.Contains(err.Error(), "database connection is not available") {
		t.Fatalf("enforced: err = %v, want the path to reach the database", err)
	}
}

func TestPledgeRestAPIErrorRejects(t *testing.T) {
	s := withResourceRow(t, answering(http.StatusServiceUnavailable, `{"error":"service unavailable"}`))

	res, err := s.GetOrCreateResource(context.Background(), pledgerClaims, privateHash)
	if err == nil || errors.Is(err, ErrPrivateNoRecord) {
		t.Fatalf("err = %v, want rest-api's failure, not ErrPrivateNoRecord", err)
	}
	if res != nil {
		t.Errorf("got resource %v: a pledge would follow", res.ResourceID)
	}
}
