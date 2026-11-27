package vault

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	removeU = "9c5db5752b7c0123456789abcdef0123456789ab" // an upload
	removeT = vaultedHash                                // a torrent
)

// An upload's pledge is not removed from the site, alone or among a
// selection, frozen or not: the upload's copy in Vault is its only one, and
// its VP come back by deleting the upload. The guard comes before the freeze
// check, which would promise a removal that never comes. A torrent's pledge
// goes as before.
func TestRemoveUploadPledgeRefused(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	// rest-api knows nothing: a removal sends Vault no pledger list.
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	t.Cleanup(rest.Close)
	ru, _ := url.Parse(rest.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range vault.RegisterFlags(api.RegisterFlags(nil)) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + ru.Hostname(), "--webtor-rest-api-port=" + ru.Port(),
		"--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	h := &Handler{vault: vault.New(c, &vault.Api{}, nil, http.DefaultClient, pgCS, api.New(c, http.DefaultClient))}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if _, err := db.QueryOne(pg.Scan(&n), q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	var id uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	o := &auth.User{ID: id}
	exec(`INSERT INTO vault.user_vp (user_id, total) VALUES (?, NULL)`, id)
	for _, r := range []string{removeU, removeT} {
		exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, 1, true, 'n')`, r)
		exec(`INSERT INTO vault.pledge (resource_id, user_id, amount, funded, frozen_at) VALUES (?, ?, 1, true, now() - interval '2 days')`, r, id)
	}
	pledged := func(r string) bool {
		return count(`SELECT count(*) FROM vault.pledge WHERE resource_id = ? AND user_id = ?`, r, id) == 1
	}
	refused := func(step string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: no error", step)
		}
		if key := web.ClassifyError(err); key != "vault.uploadNotice" {
			t.Errorf("%s: %q, want vault.uploadNotice", step, key)
		}
		if !pledged(removeU) || count(`SELECT count(*) FROM vault.tx_log WHERE resource_id = ?`, removeU) != 0 {
			t.Fatalf("%s: the upload's pledge was removed", step)
		}
	}
	ctx := context.Background()

	refused("one", h.deletePledge(ctx, removeU, o))
	refused("a selection of one", h.deletePledges(ctx, []string{removeU}, o))
	exec(`UPDATE vault.pledge SET frozen_at = now() WHERE resource_id = ?`, removeU)
	refused("frozen", h.deletePledge(ctx, removeU, o))

	if err := h.deletePledge(ctx, removeT, o); err != nil {
		t.Fatalf("a torrent: %v", err)
	}
	if pledged(removeT) || count(`SELECT count(*) FROM vault.tx_log WHERE resource_id = ? AND op_type = ?`, removeT, vaultModels.OpTypeClaim) != 1 {
		t.Error("a torrent: the pledge is there, or no claim is logged")
	}
}

// The site says why an upload with no finished file is not saved; other
// refusals keep their texts.
func TestPledgeErrUploadEmpty(t *testing.T) {
	if key := web.ClassifyError(pledgeErr(errors.Wrap(vault.ErrUploadEmpty, "pledge refused"))); key != "vault.uploadEmpty" {
		t.Errorf("no finished files: %q, want vault.uploadEmpty", key)
	}
	if key := web.ClassifyError(pledgeErr(errors.Wrap(vault.ErrPrivateNoRecord, "pledge refused"))); key != "error.vault_private_no_record" {
		t.Errorf("no record: %q, want error.vault_private_no_record", key)
	}
	if e := errors.New("x"); pledgeErr(e) != e {
		t.Error("another error is not passed as it is")
	}
}
