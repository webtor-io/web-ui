package uploads

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

const gib int64 = 1 << 30

// uid is the n-th upload ID: the upload prefix, then n in hex.
func uid(n int) string { return fmt.Sprintf("9c5db5752b7c0%027x", n) }

var (
	idA = uid(0xa)
	idB = uid(0xb)
	idX = uid(0xc)
	idY = uid(0xd)
)

// expired is a write token that ran out long ago.
const expired = "now() - interval '1 hour'"

// ago and in are SQL times relative to the database clock.
func ago(d time.Duration) string {
	return fmt.Sprintf("now() - interval '%d seconds'", int(d.Seconds()))
}

func in(d time.Duration) string {
	return fmt.Sprintf("now() + interval '%d seconds'", int(d.Seconds()))
}

var free = &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0}}}

func viewer(u *auth.User) *api.Claims {
	return &api.Claims{Uid: api.GenerateSessionIDFromUser(u)}
}

// reply is what the fake rest-api answers for one ID. status 200 answers an
// upload resource, with reserved when it is set. hold, when set, keeps the
// answer until it is closed; delay keeps it that long. Either ends early when
// the request is cancelled.
type reply struct {
	status   int
	body     string
	reserved *int64
	hold     chan struct{}
	delay    time.Duration
}

func ok(n int64) reply { return reply{status: http.StatusOK, reserved: &n} }

var (
	notFound    = reply{status: http.StatusNotFound}
	unavailable = reply{status: http.StatusServiceUnavailable, body: `{"error":"down"}`}
	noRecord    = reply{status: http.StatusForbidden, body: `{"error":"x","code":"private_torrent","reason":"no_record"}`}
)

// fakeRest is rest-api's /resource/<id>. It counts requests by ID and tells
// arrived about each one. An ID with no reply set answers 503.
type fakeRest struct {
	srv     *httptest.Server
	mu      sync.Mutex
	calls   map[string]int
	replies map[string]reply
	arrived chan string
}

func (f *fakeRest) serve(w http.ResponseWriter, r *http.Request) {
	id, found := strings.CutPrefix(r.URL.Path, "/resource/")
	f.mu.Lock()
	f.calls[id]++
	rp, set := f.replies[id]
	f.mu.Unlock()
	select {
	case f.arrived <- id:
	default:
	}
	if !found || !set {
		rp = unavailable
	}
	if rp.hold != nil {
		select {
		case <-rp.hold:
		case <-r.Context().Done():
			return
		}
	}
	if rp.delay > 0 {
		select {
		case <-time.After(rp.delay):
		case <-r.Context().Done():
			return
		}
	}
	w.WriteHeader(rp.status)
	if rp.status != http.StatusOK {
		_, _ = io.WriteString(w, rp.body)
		return
	}
	res := map[string]any{"id": id, "name": "n", "multi_file": true, "size": 0, "files_count": 0, "private": true}
	if rp.reserved != nil {
		res["reserved"] = *rp.reserved
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (f *fakeRest) set(id string, r reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[id] = r
}

func (f *fakeRest) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func (f *fakeRest) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fakeRest) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = map[string]int{}
}

func newEnv(t *testing.T) (*Service, *pg.DB, *fakeRest) {
	t.Helper()
	db := pgtest.Start(t)
	f := &fakeRest{calls: map[string]int{}, replies: map[string]reply{}, arrived: make(chan string, 1000)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	u, _ := url.Parse(f.srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range api.RegisterFlags(nil) {
		fl.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	a := api.New(cli.NewContext(cli.NewApp(), fs, nil), f.srv.Client())
	return &Service{pg: pgtest.CS(t, db), api: a}, db, f
}

func newUser(t *testing.T, db *pg.DB) *auth.User {
	t.Helper()
	var id uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	return &auth.User{ID: id}
}

// row writes a reserve row; writeUntil and gone are SQL expressions.
func row(t *testing.T, db *pg.DB, u *auth.User, id string, bytes int64, writeUntil, gone string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until, gone_at) VALUES (?, ?, ?, `+writeUntil+`, `+gone+`)`,
		u.ID, id, bytes); err != nil {
		t.Fatal(err)
	}
}

func reserveOf(t *testing.T, db *pg.DB, u *auth.User, id string) *models.UploadReserve {
	t.Helper()
	r := &models.UploadReserve{UserID: u.ID, ResourceID: id}
	if err := db.Model(r).WherePK().Select(); err != nil {
		t.Fatalf("reserve row of %s: %v", id, err)
	}
	return r
}

func hasReserve(t *testing.T, db *pg.DB, u *auth.User, id string) bool {
	t.Helper()
	ok, err := db.Model((*models.UploadReserve)(nil)).Where("user_id = ? AND resource_id = ?", u.ID, id).Exists()
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// quota is the sum with live checks, as a token request runs it.
func quota(t *testing.T, s *Service, u *auth.User) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	used, _, err := s.Quota(ctx, u, free, viewer(u), "", true)
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	return used
}

// issue runs what a token request for id runs: the checks, then the reserve
// under the lock.
func issue(t *testing.T, s *Service, db *pg.DB, u *auth.User, id string, reported, want int64) (lim, used int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, limit, err := s.Quota(ctx, u, free, viewer(u), id, true)
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	err = db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		var err error
		lim, used, err = Reserve(ctx, tx, u.ID, id, limit, reported, want, time.Now().Add(15*time.Minute))
		return err
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	return lim, used
}

func TestQuotaLeftover(t *testing.T) {
	s, db, f := newEnv(t)
	f.set(idA, ok(3*gib))
	f.set(idB, ok(4*gib))
	setup := func(t *testing.T) *auth.User {
		u := newUser(t, db)
		row(t, db, u, idA, 3*gib, expired, "NULL")
		row(t, db, u, idB, 4*gib, expired, "NULL")
		return u
	}
	t.Run("a new ID gets what is left", func(t *testing.T) {
		u := setup(t)
		lim, used := issue(t, s, db, u, uid(3), 0, FreeCap)
		if lim != 3*gib || used != 7*gib {
			t.Errorf("lim %d, used %d; want %d, %d", lim, used, 3*gib, 7*gib)
		}
	})
	t.Run("an upload's own reserve is not taken from the cap", func(t *testing.T) {
		u := setup(t)
		if lim, _ := issue(t, s, db, u, idA, 3*gib, FreeCap); lim != 6*gib {
			t.Errorf("lim %d, want %d", lim, 6*gib)
		}
	})
}

func TestQuotaWeighsDeclaredParts(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 10*gib, expired, "NULL")
	f.set(idA, ok(9*gib)) // size 0: every byte is in declared parts
	if lim, _ := issue(t, s, db, u, uid(2), 0, FreeCap); lim != 1*gib {
		t.Errorf("lim %d, want %d", lim, 1*gib)
	}
	if r := reserveOf(t, db, u, idA); r.ReservedBytes != 9*gib {
		t.Errorf("reserve of the checked upload %d, want %d", r.ReservedBytes, 9*gib)
	}
}

func TestQuotaLiveRowIsNotAsked(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 5*gib, in(10*time.Minute), "NULL")
	f.set(idA, ok(0))
	if lim, _ := issue(t, s, db, u, uid(2), 0, FreeCap); lim != 5*gib {
		t.Errorf("lim %d, want %d", lim, 5*gib)
	}
	if n := f.count(idA); n != 0 {
		t.Errorf("rest-api asked about a live reserve %d times", n)
	}
}

func TestReserveSerializesTokens(t *testing.T) {
	_, db, _ := newEnv(t)
	u := newUser(t, db)
	// Each transaction waits here for the other one, up to 300 ms: with the
	// lock the second cannot arrive while the first holds it.
	var mu sync.Mutex
	arrived := 0
	both := make(chan struct{})
	afterQuotaLock = func() {
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { afterQuotaLock = func() {} })

	ctx := context.Background()
	lims := make(chan int64, 2)
	errs := make(chan error, 2)
	for _, id := range []string{uid(1), uid(2)} {
		go func() {
			errs <- db.RunInTransaction(ctx, func(tx *pg.Tx) error {
				lim, _, err := Reserve(ctx, tx, u.ID, id, FreeCap, 0, 8*gib, time.Now().Add(15*time.Minute))
				lims <- lim
				return err
			})
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got := map[int64]bool{<-lims: true, <-lims: true}
	if !got[8*gib] || !got[2*gib] {
		t.Errorf("lims %v, want 8 GiB and 2 GiB", got)
	}
}

func TestReserveRenewalKeepsReserve(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 10*gib, in(10*time.Minute), "NULL")
	f.set(idA, ok(2*gib))
	if lim, _ := issue(t, s, db, u, idA, 2*gib, 1); lim != 10*gib {
		t.Errorf("renewal lim %d, want %d", lim, 10*gib)
	}
	if r := reserveOf(t, db, u, idA); r.ReservedBytes != 10*gib {
		t.Errorf("reserve after renewal %d, want %d", r.ReservedBytes, 10*gib)
	}
	if lim, _ := issue(t, s, db, u, uid(2), 0, FreeCap); lim > 0 {
		t.Errorf("a new ID got lim %d with the cap reserved", lim)
	}
	if hasReserve(t, db, u, uid(2)) {
		t.Error("a refused ID got a row")
	}
}

func TestReserveRenewalDoesNotGrow(t *testing.T) {
	s, db, f := newEnv(t)
	f.set(idA, ok(2*gib)) // the seeder holds 2 GiB; 4 GiB of the drop are not started
	t.Run("live reserve", func(t *testing.T) {
		u := newUser(t, db)
		row(t, db, u, idA, 6*gib, in(10*time.Minute), "NULL")
		if lim, _ := issue(t, s, db, u, idA, 2*gib, 0); lim != 6*gib {
			t.Errorf("renewal lim %d, want %d", lim, 6*gib)
		}
		if r := reserveOf(t, db, u, idA); r.ReservedBytes != 6*gib {
			t.Errorf("reserve after renewal %d, want %d", r.ReservedBytes, 6*gib)
		}
		if lim, _ := issue(t, s, db, u, uid(2), 0, FreeCap); lim != 4*gib {
			t.Errorf("a new ID got lim %d, want %d", lim, 4*gib)
		}
	})
	t.Run("expired reserve", func(t *testing.T) {
		u := newUser(t, db)
		row(t, db, u, idA, 6*gib, ago(WriteGrace+time.Minute), "NULL")
		if lim, _ := issue(t, s, db, u, idA, 2*gib, 0); lim != 2*gib {
			t.Errorf("renewal lim %d, want %d", lim, 2*gib)
		}
		if r := reserveOf(t, db, u, idA); r.ReservedBytes != 2*gib {
			t.Errorf("reserve after renewal %d, want %d", r.ReservedBytes, 2*gib)
		}
	})
}

func TestReserveSecondTab(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 6*gib, in(10*time.Minute), "NULL")
	f.set(idA, ok(2*gib))
	if lim, _ := issue(t, s, db, u, idA, 2*gib, 3*gib); lim != 9*gib {
		t.Errorf("lim %d, want %d", lim, 9*gib)
	}
}

func TestCheckDuringIssue(t *testing.T) {
	s, db, f := newEnv(t)
	for _, c := range []struct {
		name   string
		answer reply
	}{
		{"reserved", ok(1 * gib)},
		{"not found", notFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := newUser(t, db)
			row(t, db, u, idX, 1*gib, expired, "NULL")
			release := make(chan struct{})
			rp := c.answer
			rp.hold = release
			f.set(idX, rp)
			done := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _, err := s.Quota(ctx, u, free, viewer(u), "", true)
				done <- err
			}()
			select {
			case <-f.arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("the check never asked rest-api")
			}
			exp := time.Now().Add(15 * time.Minute)
			ctx := context.Background()
			if err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
				_, _, err := Reserve(ctx, tx, u.ID, idX, FreeCap, 0, 5*gib, exp)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("Quota: %v", err)
			}
			r := reserveOf(t, db, u, idX)
			if r.ReservedBytes != 5*gib || r.GoneAt != nil || r.WriteUntil == nil || r.WriteUntil.Sub(exp).Abs() > time.Second {
				t.Errorf("row %d bytes, write_until %v, gone_at %v; want %d, %v, nil", r.ReservedBytes, r.WriteUntil, r.GoneAt, 5*gib, exp)
			}
		})
	}
}

func TestDeletedWhileTokenLives(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	ctx := context.Background()
	row(t, db, u, idX, 4*gib, in(time.Minute), "NULL")
	if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'n', 0, 0)`, idX); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO library (user_id, resource_id, name) VALUES (?, ?, 'n')`, u.ID, idX); err != nil {
		t.Fatal(err)
	}
	if err := models.RemoveFromLibrary(ctx, db, u.ID, idX); err != nil {
		t.Fatal(err)
	}
	if err := models.PurgeResourceByID(ctx, db, idX); err != nil {
		t.Fatal(err)
	}
	if !hasReserve(t, db, u, idX) {
		t.Fatal("removing the library row and purging the resource took the reserve row")
	}
	if used := quota(t, s, u); used != 4*gib {
		t.Errorf("used %d while the token lives, want %d", used, 4*gib)
	}
	if n := f.count(idX); n != 0 {
		t.Errorf("rest-api asked %d times while the token lives", n)
	}

	if _, err := db.Exec(`UPDATE upload_reserve SET write_until = `+ago(WriteGrace+time.Minute)+` WHERE user_id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	f.set(idX, notFound)
	if used := quota(t, s, u); used != 0 {
		t.Errorf("used %d after the grace, want 0", used)
	}
	if r := reserveOf(t, db, u, idX); r.GoneAt == nil {
		t.Error("the gone upload is not marked")
	}
}

func TestGoneMarkOnLiveRow(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idX, 4*gib, in(time.Minute), "now()")
	row(t, db, u, idY, 2*gib, "NULL", "NULL")
	f.set(idY, ok(2*gib))
	if used := quota(t, s, u); used != 6*gib {
		t.Errorf("used %d, want %d", used, 6*gib)
	}
	if nx, ny := f.count(idX), f.count(idY); nx != 0 || ny != 1 {
		t.Errorf("rest-api asked X %d and Y %d times, want 0 and 1", nx, ny)
	}
}

func TestSavedUploadIsNotCounted(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idX, 5*gib, expired, "NULL")
	if _, err := db.Exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, name) VALUES (?, 1, 1, 'n')`, idX); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO vault.pledge (resource_id, user_id, amount) VALUES (?, ?, 1)`, idX, u.ID); err != nil {
		t.Fatal(err)
	}
	if used := quota(t, s, u); used != 0 {
		t.Errorf("used %d, want 0", used)
	}
	if n := f.count(idX); n != 0 {
		t.Errorf("rest-api asked about a saved upload %d times", n)
	}
}

func TestGoneIsFinal(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idX, 4*gib, expired, "NULL")
	// restart puts the row back unmarked with this token end and clears the
	// request count.
	restart := func(t *testing.T, writeUntil string) {
		t.Helper()
		if _, err := db.Exec(`UPDATE upload_reserve SET gone_at = NULL, write_until = `+writeUntil+` WHERE user_id = ?`, u.ID); err != nil {
			t.Fatal(err)
		}
		f.reset()
	}
	marked := func(t *testing.T) bool {
		t.Helper()
		return reserveOf(t, db, u, idX).GoneAt != nil
	}

	t.Run("not found marks it and is not asked again", func(t *testing.T) {
		f.set(idX, notFound)
		if used := quota(t, s, u); used != 0 || !marked(t) {
			t.Errorf("used %d, marked %v; want 0, true", used, marked(t))
		}
		quota(t, s, u)
		if n := f.count(idX); n != 1 {
			t.Errorf("asked %d times, want 1", n)
		}
	})
	t.Run("inside the grace after the token", func(t *testing.T) {
		restart(t, ago(WriteGrace-time.Minute))
		if used := quota(t, s, u); used != 4*gib || marked(t) || f.count(idX) != 0 {
			t.Errorf("used %d, marked %v, asked %d times; want %d, false, 0", used, marked(t), f.count(idX), 4*gib)
		}
	})
	t.Run("past the grace", func(t *testing.T) {
		restart(t, ago(WriteGrace+time.Minute))
		quota(t, s, u)
		if n := f.count(idX); n != 1 || !marked(t) {
			t.Errorf("asked %d times, marked %v; want 1, true", n, marked(t))
		}
	})
	t.Run("no record marks it", func(t *testing.T) {
		restart(t, expired)
		f.set(idX, noRecord)
		quota(t, s, u)
		if !marked(t) {
			t.Error("not marked")
		}
	})
}

func TestCheckFailureKeepsReserve(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idX, 3*gib, expired, "NULL")
	slow := ok(0)
	slow.delay = 200 * time.Millisecond
	for _, c := range []struct {
		name   string
		answer reply
	}{
		{"unavailable", unavailable},
		{"slower than a check", slow},
		{"no reserved", reply{status: http.StatusOK}},
	} {
		t.Run(c.name, func(t *testing.T) {
			saved := checkTimeout
			checkTimeout = 50 * time.Millisecond
			t.Cleanup(func() { checkTimeout = saved })
			f.set(idX, c.answer)
			used := quota(t, s, u)
			r := reserveOf(t, db, u, idX)
			if used != 3*gib || r.ReservedBytes != 3*gib || r.GoneAt != nil {
				t.Errorf("used %d, row %d, gone_at %v; want %d, %d, nil", used, r.ReservedBytes, r.GoneAt, 3*gib, 3*gib)
			}
		})
	}
	t.Run("rest-api answers nothing until the deadline", func(t *testing.T) {
		f.set(idX, reply{hold: make(chan struct{})})
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		used, _, err := s.Quota(ctx, u, free, viewer(u), "", true)
		if took := time.Since(start); took > time.Second {
			t.Errorf("Quota took %v past a 200 ms deadline", took)
		}
		if err != nil || used != 3*gib {
			t.Errorf("used %d, err %v; want %d, nil", used, err, 3*gib)
		}
	})
}

func TestCheckOrderIsRandom(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	for n := 1; n <= 20; n++ {
		row(t, db, u, uid(n), 1*gib, expired, "NULL")
		f.set(uid(n), reply{status: http.StatusServiceUnavailable, body: `{"error":"down"}`, delay: time.Second})
	}
	// A 20 ms deadline lets only the first four checks start: the last row
	// is missed by all 100 calls with chance 0.8^100.
	for range 100 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if _, _, err := s.Quota(ctx, u, free, viewer(u), "", true); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
	}
	if f.count(uid(20)) == 0 {
		t.Error("the last row was never checked")
	}
}

func TestQuotaWithoutCheckAsksNothing(t *testing.T) {
	s, db, f := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 1*gib, expired, "NULL")
	row(t, db, u, idB, 2*gib, expired, "NULL")
	used, limit, err := s.Quota(context.Background(), u, free, nil, "", false)
	if err != nil || used != 3*gib || limit != FreeCap {
		t.Errorf("Quota = %d, %d, %v; want %d, %d, nil", used, limit, err, 3*gib, FreeCap)
	}
	if n := f.total(); n != 0 {
		t.Errorf("rest-api asked %d times", n)
	}
}

func TestReserveRowsGoWithUser(t *testing.T) {
	_, db, _ := newEnv(t)
	u := newUser(t, db)
	row(t, db, u, idA, 1*gib, expired, "NULL")
	row(t, db, u, idB, 1*gib, in(10*time.Minute), "now()")
	if _, err := db.Exec(`DELETE FROM "user" WHERE user_id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if _, err := db.QueryOne(pg.Scan(&n), `SELECT count(*) FROM upload_reserve`); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d reserve rows outlived their account", n)
	}
}
