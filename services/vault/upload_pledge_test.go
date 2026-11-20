package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/golang-jwt/jwt/v5"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/private_torrent"
	"github.com/webtor-io/web-ui/services/uploads"
)

// An upload is pledged at once, its write token alive or not: web-ui first
// closes it for writes on the seeder, then reads rest-api again and takes the
// pledge's VP from what is reserved after the close.

const gib int64 = 1 << 30

// uploadAnswer is rest-api's answer for uploadHash: files finished, size,
// and reserved, which an upload always carries.
func uploadAnswer(files int, size, reserved int64) http.HandlerFunc {
	return answering(http.StatusOK, fmt.Sprintf(`{"id":%q,"name":"n","size":%d,"files_count":%d,"private":true,"reserved":%d}`,
		uploadHash, size, files, reserved))
}

// noReserved is rest-api's 200 for uploadHash without reserved: a broken or
// foreign link, never rest-api's own answer for an upload.
var noReserved = answering(http.StatusOK, fmt.Sprintf(`{"id":%q,"name":"n","size":1073741824,"files_count":1,"private":true}`, uploadHash))

// restReserved: rest-api answers the upload with before reserved until the
// seeder got its first PUT, and with after from then on.
func restReserved(t *testing.T, e *uidsEnv, before, after int64) {
	t.Helper()
	e.setRest(func(w http.ResponseWriter, r *http.Request) {
		reserved := before
		if e.sealed() > 0 {
			reserved = after
		}
		uploadAnswer(1, 0, reserved)(w, r)
	})
}

// restAfterSeal: rest-api answers the upload with 1 GiB reserved until the
// seeder got its first PUT, and with after from then on. Called again, it
// swaps after.
func restAfterSeal(t *testing.T, e *uidsEnv, after http.HandlerFunc) {
	t.Helper()
	e.setRest(func(w http.ResponseWriter, r *http.Request) {
		if e.sealed() == 0 {
			uploadAnswer(1, 0, gib)(w, r)
			return
		}
		after(w, r)
	})
}

// issueToken reserves 1 GiB for u's upload id the way a write token does.
func issueToken(t *testing.T, e *uidsEnv, u *auth.User, id string) (lim int64, err error) {
	t.Helper()
	err = e.db.RunInTransaction(context.Background(), func(tx *pg.Tx) (err error) {
		lim, _, err = uploads.Reserve(context.Background(), tx, u.ID, id, uploads.FreeCap, 0, gib, time.Now().Add(15*time.Minute))
		return
	})
	return
}

// reserveRow is u's upload_reserve row of id as text, and whether its token
// is alive.
func reserveRow(t *testing.T, e *uidsEnv, u *auth.User, id string) (row string, alive bool) {
	t.Helper()
	if _, err := e.db.QueryOne(pg.Scan(&row, &alive), `SELECT row(reserved_bytes, write_until, gone_at)::text, write_until > now()
		FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, u.ID, id); err != nil {
		t.Fatalf("upload_reserve: %v", err)
	}
	return
}

func (e *uidsEnv) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if _, err := e.db.QueryOne(pg.Scan(&n), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (e *uidsEnv) resourceRows(t *testing.T, id string) int {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM vault.resource WHERE resource_id = ?`, id)
}

// puts is how many PUTs the journal holds, gets how many rest-api reads.
func (e *uidsEnv) puts() int {
	n := 0
	for _, c := range e.journal() {
		if strings.HasPrefix(c, "PUT ") {
			n++
		}
	}
	return n
}

func (e *uidsEnv) gets() int {
	n := 0
	for _, c := range e.journal() {
		if c == "GET /resource" {
			n++
		}
	}
	return n
}

func (e *uidsEnv) pledgeUpload(t *testing.T) (*vaultModels.Resource, error) {
	t.Helper()
	return e.s.GetOrCreateResource(context.Background(), pledgerClaims, uploadHash)
}

// An upload's pledge does not wait for private pledges to open: its owner is
// the only one who can read it, enforce or not.
func TestUploadPledgeIgnoresEnforce(t *testing.T) {
	e := newUIDsEnv(t)
	was := private_torrent.Enforce()
	t.Cleanup(func() { private_torrent.SetEnforce(was) })
	private_torrent.SetEnforce(false)

	res, err := e.pledgeUpload(t)
	if err != nil || res == nil || res.RequiredVP != 1 {
		t.Fatalf("GetOrCreateResource = %v, %v; want the resource at 1 VP", res, err)
	}
}

// The pledge seals the upload while its write token is alive, reads rest-api
// again past the minute cache the modal filled, and takes reserved after the
// seal; the token's reserve row is left alone.
func TestUploadPledgeSeals(t *testing.T) {
	e := newUIDsEnv(t)
	restReserved(t, e, 9*gib, gib)
	u := e.user(t)
	if lim, err := issueToken(t, e, u, uploadHash); err != nil || lim <= 0 {
		t.Fatalf("token: lim %d, %v; want a live one", lim, err)
	}
	before, alive := reserveRow(t, e, u, uploadHash)
	if !alive {
		t.Fatalf("reserve row %s: the token is not alive", before)
	}
	if _, err := e.s.GetRequiredVP(context.Background(), pledgerClaims, uploadHash); err != nil {
		t.Fatal(err)
	}
	e.clearJournal()

	res, err := e.pledgeUpload(t)
	if err != nil || res == nil || res.RequiredVP != 1 {
		t.Fatalf("GetOrCreateResource = %v, %v; want the resource at 1 VP", res, err)
	}
	want := []string{"GET /resource", "PUT /" + uploadHash + "/", "GET /resource"}
	if got := e.journal(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("journal %v, want %v", got, want)
	}
	e.mu.Lock()
	token := e.tokens[0]
	e.mu.Unlock()
	c := &api.Claims{}
	if _, err := jwt.ParseWithClaims(token, c, func(*jwt.Token) (any, error) { return []byte("test"), nil }); err != nil {
		t.Fatalf("the seal's token: %v", err)
	}
	if !c.Internal || c.Hash != uploadHash || c.Up != "add" || c.Lim != 1 || c.Uid != "" {
		t.Errorf("the seal's token: internal %v, hash %q, up %q, lim %d, uid %q; want web-ui's service token for the upload",
			c.Internal, c.Hash, c.Up, c.Lim, c.Uid)
	}
	if after, _ := reserveRow(t, e, u, uploadHash); after != before {
		t.Errorf("reserve row %s, was %s", after, before)
	}
}

// A stranger's pledge is refused by rest-api before anything is sealed.
func TestUploadPledgeStrangerDoesNotSeal(t *testing.T) {
	e := newUIDsEnv(t)
	e.setRest(answering(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`))

	if _, err := e.pledgeUpload(t); !errors.Is(err, ErrPrivateNoRecord) {
		t.Fatalf("err = %v, want ErrPrivateNoRecord", err)
	}
	if e.puts() != 0 || e.resourceRows(t, uploadHash) != 0 {
		t.Errorf("journal %v, vault.resource rows %d; want no PUT and no row", e.journal(), e.resourceRows(t, uploadHash))
	}
}

// A second pledge, or one after a refusal for VP, finds the row and does not
// seal again.
func TestUploadPledgeExistingRowDoesNotSeal(t *testing.T) {
	e := newUIDsEnv(t)
	if _, err := vaultModels.CreateResource(context.Background(), e.db, uploadHash, 3, "n"); err != nil {
		t.Fatal(err)
	}

	res, err := e.pledgeUpload(t)
	if err != nil || res == nil || res.RequiredVP != 3 {
		t.Fatalf("GetOrCreateResource = %v, %v; want the row at 3 VP", res, err)
	}
	if e.puts() != 0 || e.resourceRows(t, uploadHash) != 1 {
		t.Errorf("journal %v, rows %d; want no PUT and the one row", e.journal(), e.resourceRows(t, uploadHash))
	}
}

// The seeder's 410: the upload went between the owner's check and the seal.
func TestUploadPledgeSealGone(t *testing.T) {
	e := newUIDsEnv(t)
	e.setSealStatus(http.StatusGone)

	if _, err := e.pledgeUpload(t); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("err = %v, want ErrResourceNotFound", err)
	}
	if e.resourceRows(t, uploadHash) != 0 || e.gets() != 1 {
		t.Errorf("rows %d, journal %v; want no row and rest-api read once", e.resourceRows(t, uploadHash), e.journal())
	}
}

// A failed seal is no pledge; once the seeder answers, the pledge goes.
func TestUploadPledgeSealFails(t *testing.T) {
	e := newUIDsEnv(t)
	e.setSealStatus(http.StatusInternalServerError)

	if _, err := e.pledgeUpload(t); err == nil || errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("err = %v, want the seal's failure", err)
	}
	if n := e.resourceRows(t, uploadHash); n != 0 {
		t.Fatalf("%d vault.resource rows after a failed seal", n)
	}

	e.setSealStatus(http.StatusNoContent)
	if res, err := e.pledgeUpload(t); err != nil || res == nil {
		t.Fatalf("retry: %v, %v", res, err)
	}
	if n := e.puts(); n != 2 {
		t.Errorf("%d PUTs, want 2", n)
	}
}

// Refused for VP: the row stays, the upload stays sealed, no token is given,
// and a pledge with points later takes the same row without a second seal.
func TestUploadPledgeAfterVPRefusal(t *testing.T) {
	e := newUIDsEnv(t)
	u := e.user(t)
	setTotal := func(total float64) {
		if _, err := e.db.Exec(`UPDATE vault.user_vp SET total = ? WHERE user_id = ?`, total, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	pledges := func() int {
		return e.count(t, `SELECT count(*) FROM vault.pledge WHERE resource_id = ?`, uploadHash)
	}
	setTotal(0)

	res, err := e.pledgeUpload(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreatePledge(context.Background(), u, res); err == nil || !strings.Contains(err.Error(), "insufficient vault points") {
		t.Fatalf("CreatePledge with 0 VP: %v, want insufficient vault points", err)
	}
	if e.resourceRows(t, uploadHash) != 1 || pledges() != 0 || e.puts() != 1 {
		t.Fatalf("rows %d, pledges %d, journal %v; want the row, no pledge, one PUT", e.resourceRows(t, uploadHash), pledges(), e.journal())
	}
	if _, err := issueToken(t, e, u, uploadHash); !errors.Is(err, uploads.ErrFrozen) {
		t.Errorf("a token after the seal: %v, want ErrFrozen", err)
	}

	setTotal(10)
	res, err = e.pledgeUpload(t)
	if err != nil || res == nil || res.ResourceID != uploadHash {
		t.Fatalf("second GetOrCreateResource = %v, %v", res, err)
	}
	if e.resourceRows(t, uploadHash) != 1 || e.puts() != 1 {
		t.Errorf("rows %d, journal %v; want the one row and still one PUT", e.resourceRows(t, uploadHash), e.journal())
	}
	if _, err := e.s.CreatePledge(context.Background(), u, res); err != nil {
		t.Errorf("CreatePledge with 10 VP: %v", err)
	}
}

// rest-api's 200 without reserved is a broken link's answer: no seal, no row.
func TestUploadPledgeWithoutReserved(t *testing.T) {
	e := newUIDsEnv(t)
	e.setRest(noReserved)

	if _, err := e.pledgeUpload(t); err == nil {
		t.Fatal("no error")
	}
	if e.resourceRows(t, uploadHash) != 0 || e.puts() != 0 {
		t.Errorf("rows %d, journal %v; want neither a row nor a PUT", e.resourceRows(t, uploadHash), e.journal())
	}
}

// A row left by a refusal for VP is not returned for an upload rest-api no
// longer has, or answers without reserved: the pledge would take VP for it.
func TestUploadPledgeGoneWithRow(t *testing.T) {
	for _, c := range []struct {
		name     string
		rest     http.HandlerFunc
		notFound bool
	}{
		{"gone", answering(http.StatusNotFound, `{"error":"not found"}`), true},
		{"no reserved", noReserved, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newUIDsEnv(t)
			if _, err := vaultModels.CreateResource(context.Background(), e.db, uploadHash, 1, "n"); err != nil {
				t.Fatal(err)
			}
			e.setRest(c.rest)

			res, err := e.pledgeUpload(t)
			if err == nil || errors.Is(err, ErrResourceNotFound) != c.notFound {
				t.Fatalf("err = %v, want an error, ErrResourceNotFound %v", err, c.notFound)
			}
			if res != nil {
				t.Errorf("returned the row %v", res.ResourceID)
			}
			if n := e.count(t, `SELECT count(*) FROM vault.pledge WHERE resource_id = ?`, uploadHash); e.puts() != 0 || n != 0 {
				t.Errorf("journal %v, pledges %d; want no PUT, no pledge", e.journal(), n)
			}
		})
	}
}

// rest-api fails on the read after the seal: an error, no panic, no row; the
// retry seals again (the seal is idempotent) and goes.
func TestUploadPledgeSecondReadFails(t *testing.T) {
	for _, c := range []struct {
		name     string
		after    http.HandlerFunc
		notFound bool
	}{
		{"gone", answering(http.StatusNotFound, `{"error":"not found"}`), true},
		{"no reserved", noReserved, false},
		{"rest-api down", answering(http.StatusServiceUnavailable, `{"error":"x"}`), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newUIDsEnv(t)
			restAfterSeal(t, e, c.after)

			// A failure is not "gone": rest-api's 503 swallowed reads as
			// nil, which only the error tells from a 404.
			res, err := e.pledgeUpload(t)
			if err == nil || errors.Is(err, ErrResourceNotFound) != c.notFound {
				t.Fatalf("err = %v, want an error, ErrResourceNotFound %v", err, c.notFound)
			}
			if res != nil || e.resourceRows(t, uploadHash) != 0 || e.puts() != 1 {
				t.Fatalf("res %v, rows %d, journal %v; want no row and one PUT", res, e.resourceRows(t, uploadHash), e.journal())
			}

			restAfterSeal(t, e, uploadAnswer(1, gib, gib))
			res, err = e.pledgeUpload(t)
			if err != nil || res == nil || res.RequiredVP != 1 {
				t.Fatalf("retry: %v, %v; want the resource at 1 VP", res, err)
			}
			if n := e.puts(); n != 2 {
				t.Errorf("%d PUTs, want 2", n)
			}
		})
	}
}

// An upload's ID in upper case is no upload's for web-ui, though rest-api
// would answer it as the upload: refused before any request. A torrent's ID
// in upper case goes on as before.
func TestUploadPledgeUpperCase(t *testing.T) {
	e := newUIDsEnv(t)
	upper := strings.ToUpper(uploadHash)

	if _, err := e.s.GetOrCreateResource(context.Background(), pledgerClaims, upper); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("err = %v, want ErrResourceNotFound", err)
	}
	if j := e.journal(); len(j) != 0 {
		t.Errorf("journal %v, want nothing asked", j)
	}
	if n := e.resourceRows(t, upper) + e.resourceRows(t, uploadHash); n != 0 {
		t.Errorf("%d vault.resource rows", n)
	}

	if res, err := e.s.GetOrCreateResource(context.Background(), pledgerClaims, strings.ToUpper(publicHash)); err != nil || res == nil {
		t.Errorf("a torrent in upper case: %v, %v; want it to go on", res, err)
	}
}

// Without uploads there is no seal, so there is no pledge.
func TestUploadPledgeUploadsOff(t *testing.T) {
	e := newUIDsEnv(t)
	e.s.WithUploads(nil)

	if _, err := e.pledgeUpload(t); err == nil {
		t.Fatal("no error")
	}
	if n := e.resourceRows(t, uploadHash); n != 0 {
		t.Errorf("%d vault.resource rows", n)
	}
}

// The stats split the funded points between the account's own files and
// torrents.
func TestUserStatsSplitByClass(t *testing.T) {
	e := newUIDsEnv(t)
	if _, err := vaultModels.CreateResource(context.Background(), e.db, uploadHash, 1, "n"); err != nil {
		t.Fatal(err)
	}
	a := e.user(t)
	e.pledge(t, a, uploadHash)
	e.pledge(t, a, publicHash)

	stats, _, err := e.s.GetUserStats(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FundedOwnFiles != 1 || stats.FundedTorrents != 1 {
		t.Errorf("own files %v, torrents %v; want 1 and 1", stats.FundedOwnFiles, stats.FundedTorrents)
	}
}

// An upload with no finished file is not pledged, whatever its declared
// parts: nothing would be saved, and the seal would end the upload for good.
// The check comes before the seal and before the row's early return; once a
// file is finished, the pledge goes.
func TestUploadPledgeEmpty(t *testing.T) {
	for _, c := range []struct {
		name     string
		reserved int64
		token    bool
		row      bool
	}{
		{"a part declared", 5 * gib, true, false},
		{"empty", 0, false, false},
		{"a part declared, a row", 5 * gib, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newUIDsEnv(t)
			e.setRest(uploadAnswer(0, 0, c.reserved))
			if c.token {
				if lim, err := issueToken(t, e, e.user(t), uploadHash); err != nil || lim <= 0 {
					t.Fatalf("token: lim %d, %v; want a live one", lim, err)
				}
			}
			rows := 0
			if c.row {
				rows = 1
				if _, err := vaultModels.CreateResource(context.Background(), e.db, uploadHash, 1, "n"); err != nil {
					t.Fatal(err)
				}
			}

			res, err := e.pledgeUpload(t)
			if !errors.Is(err, ErrUploadEmpty) || res != nil {
				t.Fatalf("GetOrCreateResource = %v, %v; want ErrUploadEmpty", res, err)
			}
			if j := e.journal(); fmt.Sprint(j) != fmt.Sprint([]string{"GET /resource"}) {
				t.Errorf("journal %v, want rest-api read once and no seal", j)
			}
			if n := e.resourceRows(t, uploadHash); n != rows {
				t.Errorf("%d vault.resource rows, want %d", n, rows)
			}
			if c.name != "a part declared" {
				return
			}

			e.setRest(uploadAnswer(1, gib, gib))
			res, err = e.pledgeUpload(t)
			if err != nil || res == nil || res.RequiredVP != 1 {
				t.Fatalf("a file finished: %v, %v; want the resource at 1 VP", res, err)
			}
			if n := e.puts(); n != 1 {
				t.Errorf("%d PUTs, want 1", n)
			}
		})
	}
}
