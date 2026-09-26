package resource

import (
	"context"
	"errors"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/statusview"
)

const ssHash = "08ada5a7a6183aae1e09d831df6748d566095a10"

func exportWith(items map[string]string) *ra.ExportResponse {
	e := &ra.ExportResponse{ExportItems: map[string]ra.ExportItem{}}
	for k, u := range items {
		e.ExportItems[k] = ra.ExportItem{Type: k, URL: u}
	}
	return e
}

// The host is the node rest-api picked for this torrent — taken from an
// export URL of the same response, never rebuilt here. Scheme, host, a path
// prefix and the api-key stay; the export's own token does not.
func TestSessionStatsTarget(t *testing.T) {
	stat := "https://abc1.api.example.io/" + ssHash + "/?api-key=k&stats=true&token=T"
	dl := "https://abc1.api.example.io/" + ssHash + "/Movie~arch/Movie.zip?api-key=k&download=true&token=T"
	stream := "https://abc2.api.example.io/" + ssHash + "/Movie/a.mkv~hls/index.m3u8?api-key=k2&token=T"
	cases := []struct {
		name  string
		items map[string]string
		want  sessionTarget
	}{
		{"stat item wins", map[string]string{"torrent_client_stat": stat, "download": dl},
			sessionTarget{base: "https://abc1.api.example.io/session-stats/" + ssHash, apiKey: "k"}},
		// Cached content: rest-api drops the stat item — exactly where the
		// plan is often the only limit, so the download item stands in.
		{"cached: download", map[string]string{"download": dl, "stream": stream},
			sessionTarget{base: "https://abc1.api.example.io/session-stats/" + ssHash, apiKey: "k"}},
		{"only stream", map[string]string{"stream": stream},
			sessionTarget{base: "https://abc2.api.example.io/session-stats/" + ssHash, apiKey: "k2"}},
		{"empty stat URL falls through", map[string]string{"torrent_client_stat": "", "download": dl},
			sessionTarget{base: "https://abc1.api.example.io/session-stats/" + ssHash, apiKey: "k"}},
		// Self-hosted signs export URLs under a path prefix that its nginx
		// strips; the prefix is part of the route to thp. No key there.
		{"path prefix kept", map[string]string{"download": "http://localhost:8080/torrent-http-proxy/" + ssHash + "/a.mkv?download=true&token=T"},
			sessionTarget{base: "http://localhost:8080/torrent-http-proxy/session-stats/" + ssHash}},
		// A URL that is not about this torrent is not this torrent's node.
		{"other torrent's URL", map[string]string{"download": "https://abc1.api.example.io/ffffffffffffffffffffffffffffffffffffffff/a.mkv?token=T"}, sessionTarget{}},
		{"nothing pointing at thp", map[string]string{"subtitles": "https://abc1.api.example.io/" + ssHash + "/a.mkv~vi/subtitles.json?token=T"}, sessionTarget{}},
		{"no items", map[string]string{}, sessionTarget{}},
	}
	for _, c := range cases {
		if got := sessionStatsTarget(exportWith(c.items), ssHash); got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
	if sessionStatsTarget(nil, ssHash).ok() {
		t.Error("nil export → no target")
	}
	// The URL carries the minted token and nothing of the export's query.
	u, err := url.Parse(sessionStatsTarget(exportWith(map[string]string{"download": dl}), ssHash).url("MINTED"))
	if err != nil {
		t.Fatal(err)
	}
	if q := u.Query(); q.Get("token") != "MINTED" || q.Get("api-key") != "k" || len(q) != 2 {
		t.Errorf("query %v", q)
	}
}

const testSecret = "api-secret"

func testSign(c jwt.Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(testSecret))
}

func parseToken(t *testing.T, tok string) jwt.MapClaims {
	t.Helper()
	m := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tok, m, func(*jwt.Token) (any, error) { return []byte(testSecret), nil }); err != nil {
		t.Fatal(err)
	}
	return m
}

// The token thp opens the stream for: the viewer's standard claims — the
// same sessionID and domain the page's export URLs carry, so the counters
// are the viewer's — bound to this torrent in lower case, expiring in ten
// minutes, and nothing else: no scope claim, no grace rules.
func TestSessionStatsToken(t *testing.T) {
	now := time.Now()
	cl := &api.Claims{SessionID: "s1", Domain: "webtor.example", Rate: "5M", Role: "free", Agent: "UA", RemoteAddress: "10.0.0.1",
		Rules: []api.Rule{{Kind: "grace"}}}
	tok, err := sessionStatsToken(testSign, cl, strings.ToUpper(ssHash), now)
	if err != nil {
		t.Fatal(err)
	}
	m := parseToken(t, tok.tok)
	if m["sessionID"] != "s1" || m["domain"] != "webtor.example" || m["hash"] != ssHash || m["rate"] != "5M" {
		t.Errorf("claims %v", m)
	}
	if _, ok := m["scope"]; ok {
		t.Error("the contract has no scope claim")
	}
	if _, ok := m["rules"]; ok {
		t.Error("grace rules do not belong on this token")
	}
	exp, _ := m.GetExpirationTime()
	if exp == nil || exp.Sub(now) > sessionTokenTTL || exp.Sub(now) < sessionTokenTTL-time.Second {
		t.Errorf("exp %v, want now+%v", exp, sessionTokenTTL)
	}
	// The watch rotates by the expiry thp will act on: the signed one.
	if exp == nil || !tok.exp.Equal(exp.Time) {
		t.Errorf("reported exp %v, signed %v", tok.exp, exp)
	}
	if cl.Hash != "" || cl.Rules == nil {
		t.Error("the request's own claims must not change")
	}
	if _, err := sessionStatsToken(testSign, nil, ssHash, now); err == nil {
		t.Error("no claims, no token")
	}
	if _, err := sessionStatsToken(testSign, &api.Claims{Domain: "d"}, ssHash, now); err == nil {
		t.Error("no session, no token: thp has nothing to read")
	}
}

// fakeOpener hands out scripted results and records each open, with the
// context the stream was opened with (a replaced stream is cancelled).
type fakeOpener struct {
	mu      sync.Mutex
	urls    []string
	ctxs    []context.Context
	results []sessionOpen
}

func (f *fakeOpener) open(ctx context.Context, u string) (<-chan api.SessionStatsData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, u)
	f.ctxs = append(f.ctxs, ctx)
	if len(f.results) == 0 {
		return nil, errors.New("no scripted result")
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r.ch, r.err
}

func (f *fakeOpener) opens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.urls)
}

// fakeClock records scheduled timers (retries, rotations) and fires the
// ones still pending on demand; a stopped timer never fires.
type fakeTimer struct {
	f           func()
	done, fired bool
}

type fakeClock struct {
	delays  []time.Duration
	timers  []*fakeTimer
	stopped int
}

func (c *fakeClock) after(d time.Duration, f func()) func() bool {
	tm := &fakeTimer{f: f}
	c.delays = append(c.delays, d)
	c.timers = append(c.timers, tm)
	return func() bool {
		c.stopped++
		if tm.done {
			return false
		}
		tm.done = true
		return true
	}
}

func (c *fakeClock) pending() int {
	n := 0
	for _, tm := range c.timers {
		if !tm.done {
			n++
		}
	}
	return n
}

func (c *fakeClock) fire() {
	for _, tm := range c.timers {
		if !tm.done {
			tm.done, tm.fired = true, true
			tm.f()
		}
	}
}

// countingMint hands out tok1, tok2, … — a new token for every open, each
// expiring sessionTokenTTL from when it is minted.
func countingMint() func() (sessionToken, error) {
	var mu sync.Mutex
	n := 0
	return func() (sessionToken, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return sessionToken{tok: "tok" + string(rune('0'+n)), exp: time.Now().Add(sessionTokenTTL)}, nil
	}
}

var testTarget = sessionTarget{base: "https://node.example/session-stats/" + ssHash, apiKey: "k"}

// newTestWatch: jitter pinned to the middle of its range (factor 1).
func newTestWatch(results ...sessionOpen) (*sessionWatch, *fakeOpener, *fakeClock) {
	op := &fakeOpener{results: results}
	clk := &fakeClock{}
	w := newSessionWatch("res", op.open, clk.after, countingMint())
	w.jitter = func() float64 { return 0.5 }
	return w, op, clk
}

// recv waits for the watch's async open to report.
func recv(t *testing.T, w *sessionWatch) sessionOpen {
	t.Helper()
	select {
	case r := <-w.results:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("open did not report")
		return sessionOpen{}
	}
}

func thr(v float64) *float64 { return &v }

// capBps is what a "5M" session is paced at.
const capBps = 5 * 1024 * 1024 / 8

// feed sends n at-cap events, one second apart from t0.
func feed(w *sessionWatch, t0 time.Time, n int) time.Time {
	ev := api.SessionStatsData{BytesPerSec: capBps, Conns: 1, Rate: "5M", Throttled: thr(0.9)}
	for i := 0; i < n; i++ {
		t0 = t0.Add(time.Second)
		w.event(context.Background(), ev, true, t0)
	}
	return t0
}

// The plan's verdict turns on only over thp's whole window, and the window
// is the one thp says (window_sec): at 1 s its ring is full from a stream's
// second event, so the fact is due at the fourth -- not at the eighth, as
// with thp's own 5 s taken for an event that says nothing.
func TestSessionWatch_WindowIsThpsWord(t *testing.T) {
	w, _, _ := newTestWatch()
	now := time.Now()
	ev := api.SessionStatsData{WindowSec: 1, BytesPerSec: capBps, Conns: 1, Rate: "5M", Throttled: thr(0.9)}
	for i := 1; i <= 4; i++ {
		now = now.Add(time.Second)
		w.event(context.Background(), ev, true, now)
	}
	if r := w.reading(now); !r.Limited {
		t.Fatalf("window_sec 1, the fourth event at the cap: %+v, want the fact", r)
	}
}

func TestSessionWatch_OpensOnceAndReadsEvents(t *testing.T) {
	ctx := context.Background()
	ch := make(chan api.SessionStatsData)
	w, op, _ := newTestWatch(sessionOpen{ch: ch})
	w.start(ctx, sessionTarget{})
	if w.started() {
		t.Fatal("no target, no stream")
	}
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	w.start(ctx, sessionTarget{base: "https://other.example/session-stats/x"}) // a stats reconnect hands it again: ignored
	select {
	case r := <-w.results:
		t.Fatalf("a second stream was opened: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}
	if op.opens() != 1 || op.urls[0] != testTarget.url("tok1") || w.ch == nil {
		t.Fatalf("want one open of the target with a minted token, got %v", op.urls)
	}
	now := feed(w, time.Now(), statusview.PlanBoxFromOpen)
	if r := w.reading(now); !r.Known || !r.Limited || r.CapMbps != 5 || r.Mbps != 5 {
		t.Errorf("fresh events must reach the reading: %+v", r)
	}
}

// Every open mints its own token: a reopen after the first one's ten minutes
// would otherwise be refused, and a stream would be lost for good.
func TestSessionWatch_MintsAFreshTokenPerOpen(t *testing.T) {
	ctx := context.Background()
	w, op, clk := newTestWatch(sessionOpen{err: &api.StatusError{Status: 502}}, sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	clk.fire()
	w.opened(ctx, recv(t, w))
	if op.opens() != 2 || op.urls[0] == op.urls[1] || !strings.Contains(op.urls[1], "token=tok2") {
		t.Fatalf("urls %v", op.urls)
	}
}

// A token that cannot be minted (no API secret, no session) is final: no
// request, no retry.
func TestSessionWatch_MintFailureIsFinal(t *testing.T) {
	ctx := context.Background()
	op := &fakeOpener{}
	clk := &fakeClock{}
	w := newSessionWatch("res", op.open, clk.after, func() (sessionToken, error) { return sessionToken{}, errors.New("api secret not configured") })
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	if op.opens() != 0 || len(clk.delays) != 0 {
		t.Fatalf("opens=%d delays=%v", op.opens(), clk.delays)
	}
}

// A 4xx is final — an old thp without the route, a token without a session,
// a refused one: asking again gets the same answer.
func TestSessionWatch_FinalAnswerIsNotRetried(t *testing.T) {
	ctx := context.Background()
	for _, code := range []int{404, 403, 400, 429} {
		w, op, clk := newTestWatch(sessionOpen{err: &api.StatusError{Status: code}})
		w.start(ctx, testTarget)
		w.opened(ctx, recv(t, w))
		if len(clk.delays) != 0 || op.opens() != 1 {
			t.Errorf("%d: no retry expected, got delays %v", code, clk.delays)
		}
	}
}

// Transient failures and a stream that ends are retried a few times with
// backoff, then given up on silently.
func TestSessionWatch_BoundedBackoff(t *testing.T) {
	ctx := context.Background()
	fail := sessionOpen{err: errors.New("dial tcp: connection refused")}
	five := sessionOpen{err: &api.StatusError{Status: 502}}
	w, op, clk := newTestWatch(fail, five, fail, fail, fail)
	w.start(ctx, testTarget)
	for i := 0; i < 10; i++ {
		w.opened(ctx, recv(t, w))
		if clk.pending() == 0 {
			break
		}
		clk.fire()
	}
	if op.opens() != 1+sessionRetries {
		t.Errorf("want %d opens, got %d", 1+sessionRetries, op.opens())
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(clk.delays) != len(want) {
		t.Fatalf("want delays %v, got %v", want, clk.delays)
	}
	for i := range want {
		if clk.delays[i] != want[i] {
			t.Errorf("delay %d: got %v, want %v", i, clk.delays[i], want[i])
		}
	}
}

// A thp rotation cuts every stream on a node at once; the reopens are spread
// over half to one and a half times the backoff instead of landing together.
func TestRetryDelay_Jitter(t *testing.T) {
	if d := retryDelay(1, 0); d != time.Second {
		t.Errorf("low end: %v", d)
	}
	if d := retryDelay(1, 0.999999); d < 2999*time.Millisecond || d >= 3*time.Second {
		t.Errorf("high end: %v", d)
	}
	if d := retryDelay(3, 0.5); d != 8*time.Second {
		t.Errorf("middle: %v", d)
	}
	w := newSessionWatch("res", nil, nil, nil)
	seen := map[time.Duration]bool{}
	for i := 0; i < 20; i++ {
		seen[retryDelay(1, w.jitter())] = true
	}
	if len(seen) < 2 {
		t.Errorf("production jitter does not vary: %v", seen)
	}
}

func TestSessionWatch_ClosedStreamIsRetriedWithinTheSameBudget(t *testing.T) {
	ctx := context.Background()
	ch := make(chan api.SessionStatsData)
	close(ch)
	w, op, clk := newTestWatch(sessionOpen{ch: ch}, sessionOpen{err: errors.New("refused")})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	w.event(ctx, api.SessionStatsData{}, false, time.Now())
	if w.ch != nil || clk.pending() != 1 {
		t.Fatalf("a closed stream schedules one retry, got pending=%d", clk.pending())
	}
	clk.fire()
	w.opened(ctx, recv(t, w))
	if op.opens() != 2 || w.retries != 2 {
		t.Errorf("opens=%d retries=%d", op.opens(), w.retries)
	}
}

// A page open through a few routine thp rollouts is not a dead endpoint: a
// reopened stream that delivered for sessionRetryReset gets the budget back.
// One that dies sooner does not.
func TestSessionWatch_RetryBudgetResetsAfterAHealthyStream(t *testing.T) {
	ctx := context.Background()
	closed := func() <-chan api.SessionStatsData { c := make(chan api.SessionStatsData); close(c); return c }
	w, _, clk := newTestWatch(sessionOpen{ch: closed()}, sessionOpen{ch: closed()}, sessionOpen{ch: closed()})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	t0 := time.Now()
	w.event(ctx, api.SessionStatsData{}, false, t0) // a pod rollout
	clk.fire()
	w.opened(ctx, recv(t, w))
	t1 := feed(w, t0, 10)
	if w.retries != 1 {
		t.Fatalf("10 s of events must not reset the budget: retries=%d", w.retries)
	}
	w.event(ctx, api.SessionStatsData{}, false, t1) // another one
	clk.fire()
	w.opened(ctx, recv(t, w))
	if w.retries != 2 {
		t.Fatalf("retries=%d", w.retries)
	}
	t2 := feed(w, t1, int(sessionRetryReset/time.Second)+1)
	if w.retries != 0 {
		t.Fatalf("a minute of events must reset the budget: retries=%d", w.retries)
	}
	// The minute is the reopened stream's own.
	w, _, clk = newTestWatch(sessionOpen{ch: closed()}, sessionOpen{ch: closed()})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	t3 := feed(w, t2, int(sessionRetryReset/time.Second)+1)
	w.event(ctx, api.SessionStatsData{}, false, t3)
	clk.fire()
	w.opened(ctx, recv(t, w))
	t3 = feed(w, t3.Add(2*time.Second), 1)
	w.event(ctx, api.SessionStatsData{}, false, t3)
	if w.retries != 2 {
		t.Fatalf("a stream that lasted one event must not reset the budget: retries=%d", w.retries)
	}
}

// A reopened stream's first event has no window again: the reading goes
// back to unknown until the second, instead of showing a zero.
func TestSessionWatch_ReopenedStreamStartsUnknown(t *testing.T) {
	ctx := context.Background()
	closed := make(chan api.SessionStatsData)
	close(closed)
	w, _, clk := newTestWatch(sessionOpen{ch: closed}, sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	now := feed(w, time.Now(), 3)
	w.event(ctx, api.SessionStatsData{}, false, now)
	clk.fire()
	w.opened(ctx, recv(t, w))
	now = now.Add(time.Second)
	w.event(ctx, api.SessionStatsData{}, true, now) // zero-length window
	if r := w.reading(now); r.Known {
		t.Fatalf("first event after a reopen read as %+v", r)
	}
}

// A retry that comes due after the status stream ended opens nothing, and
// stopping the watch stops the pending timer.
func TestSessionWatch_NoOpenAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w, op, clk := newTestWatch(sessionOpen{err: errors.New("refused")})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	cancel()
	w.stop()
	clk.fire()
	time.Sleep(20 * time.Millisecond)
	if op.opens() != 1 {
		t.Errorf("no open after cancel, got %d", op.opens())
	}
	if clk.stopped != 1 {
		t.Errorf("stop must stop the pending retry, stopped=%d", clk.stopped)
	}
}

// An open that finishes after the status stream ended does not block on the
// results channel nobody reads any more: the goroutine exits.
func TestSessionWatch_OpenGoroutineExitsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	w := newSessionWatch("res", func(context.Context, string) (<-chan api.SessionStatsData, error) {
		<-release
		return nil, errors.New("late")
	}, (&fakeClock{}).after, countingMint())
	w.target = testTarget
	w.results = make(chan sessionOpen) // unbuffered, and nobody reads it
	base := runtime.NumGoroutine()
	w.dial(ctx, openFresh)
	if runtime.NumGoroutine() != base+1 {
		t.Fatalf("dial should run one goroutine: %d → %d", base, runtime.NumGoroutine())
	}
	cancel()
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			t.Fatalf("dial's goroutine leaked: %d > %d", runtime.NumGoroutine(), base)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Numbers that stopped arriving do not stay on screen: the stream may stay
// open and silent, and the reading turns unknown after StaleAfter.
func TestSessionWatch_StaleDrop(t *testing.T) {
	w, _, _ := newTestWatch()
	t0 := feed(w, time.Now(), statusview.PlanBoxFromOpen)
	if r := w.reading(t0.Add(statusview.StaleAfter)); !r.Known || !r.Limited {
		t.Fatalf("still fresh at exactly StaleAfter: %+v", r)
	}
	if r := w.reading(t0.Add(statusview.StaleAfter + time.Millisecond)); r.Known || r.Limited {
		t.Errorf("stale numbers must go: %+v", r)
	}
}

// logged collects what the watch logs at Info and above for the rest of the
// test: a planned rotation is not news for the log, a lost stream is.
func logged(t *testing.T) *logtest.Hook {
	t.Helper()
	hook := logtest.NewGlobal()
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(make(log.LevelHooks)) })
	return hook
}

// thp ends every stream at its token's expiry. The watch opens the next one
// sessionRotateLead before that and switches to it the moment it opens: the
// old stream is cancelled, the retry budget untouched, and the reading --
// the speed and the plan's verdict -- carries straight on.
func TestSessionWatch_RotatesBeforeTheTokenExpires(t *testing.T) {
	ctx := context.Background()
	ch1, ch2 := make(chan api.SessionStatsData), make(chan api.SessionStatsData)
	w, op, clk := newTestWatch(sessionOpen{ch: ch1}, sessionOpen{ch: ch2})
	hook := logged(t)
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	if clk.pending() != 1 {
		t.Fatalf("an open stream has its rotation scheduled: pending=%d", clk.pending())
	}
	if d, want := clk.delays[0], sessionTokenTTL-sessionRotateLead; d > want || d < want-5*time.Second {
		t.Errorf("rotation due in %v, want about %v", d, want)
	}
	now := feed(w, time.Now(), statusview.PlanBoxFromOpen)
	clk.fire() // the rotation comes due
	w.opened(ctx, recv(t, w))
	if w.ch != (<-chan api.SessionStatsData)(ch2) {
		t.Fatal("the new stream did not take over")
	}
	if op.ctxs[0].Err() == nil {
		t.Error("the replaced stream is still open")
	}
	if w.retries != 0 || w.gaveUp {
		t.Errorf("a planned rotation spent the budget: retries=%d", w.retries)
	}
	// Its first event covers a zero-length window: skipped, and nothing
	// measured before is lost.
	now = now.Add(time.Second)
	w.event(ctx, api.SessionStatsData{Conns: 1, Rate: "5M"}, true, now)
	if r := w.reading(now); !r.Known || !r.Limited || !r.PlanBox || r.Mbps != 5 {
		t.Fatalf("across the rotation: %+v", r)
	}
	now = feed(w, now, 1)
	if r := w.reading(now); !r.Known || !r.Limited || !r.PlanBox || r.Mbps != 5 {
		t.Errorf("after the rotation: %+v", r)
	}
	if clk.pending() != 1 {
		t.Errorf("the new stream's own rotation: pending=%d", clk.pending())
	}
	if n := len(hook.AllEntries()); n != 0 {
		t.Errorf("a rotation logged %d lines at Info or above: %v", n, hook.LastEntry().Message)
	}
}

// A rotation that fails leaves the stream it was to replace running; when
// thp ends that one at its token's expiry it is reopened at once -- no
// backoff, no retry budget, the reading carried over.
func TestSessionWatch_StreamEndingAtItsExpiryReopensAtOnce(t *testing.T) {
	ctx := context.Background()
	ch1, ch3 := make(chan api.SessionStatsData), make(chan api.SessionStatsData)
	w, op, clk := newTestWatch(sessionOpen{ch: ch1}, sessionOpen{err: &api.StatusError{Status: 502}}, sessionOpen{ch: ch3})
	hook := logged(t)
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	feed(w, time.Now(), statusview.PlanBoxFromOpen)
	clk.fire() // the rotation, refused
	w.opened(ctx, recv(t, w))
	if w.ch == nil || !w.rotationFailed || clk.pending() != 0 {
		t.Fatalf("a failed rotation keeps the stream and waits: ch=%v failed=%v pending=%d", w.ch != nil, w.rotationFailed, clk.pending())
	}
	end := w.exp
	now := feed(w, end.Add(-3*time.Second), 3)
	w.event(ctx, api.SessionStatsData{}, false, now) // thp: the token expired
	if clk.pending() != 0 || w.retries != 0 {
		t.Fatalf("a planned end is not a failure: pending=%d retries=%d", clk.pending(), w.retries)
	}
	w.opened(ctx, recv(t, w))
	if w.ch != (<-chan api.SessionStatsData)(ch3) || op.opens() != 3 {
		t.Fatalf("reopened: opens=%d", op.opens())
	}
	now = now.Add(time.Second)
	w.event(ctx, api.SessionStatsData{Conns: 1, Rate: "5M"}, true, now)
	if r := w.reading(now); !r.Known || !r.Limited {
		t.Errorf("across the reopen: %+v", r)
	}
	if n := len(hook.AllEntries()); n != 0 {
		t.Errorf("a planned end logged %d lines at Info or above: %v", n, hook.LastEntry().Message)
	}
}

// A stream that fails inside its last sessionRotateLead, while the rotation
// is on its way: the rotation's stream takes over, and nothing else is
// dialled.
func TestSessionWatch_FailureWhileRotatingWaitsForTheRotation(t *testing.T) {
	ctx := context.Background()
	ch2 := make(chan api.SessionStatsData)
	w, op, clk := newTestWatch(sessionOpen{ch: make(chan api.SessionStatsData)}, sessionOpen{ch: ch2})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	clk.fire() // the rotation is dialling
	w.event(ctx, api.SessionStatsData{}, false, w.rotateAt.Add(time.Second))
	if clk.pending() != 0 || w.retries != 0 || !w.awaitRotation {
		t.Fatalf("pending=%d retries=%d await=%v", clk.pending(), w.retries, w.awaitRotation)
	}
	w.opened(ctx, recv(t, w))
	if w.ch != (<-chan api.SessionStatsData)(ch2) || op.opens() != 2 {
		t.Errorf("the rotation's stream did not take over: opens=%d", op.opens())
	}
}

// A rotation that reports after its stream already failed (and a reopen is
// due) is not a second stream.
func TestSessionWatch_LateRotationAfterAFailureIsDropped(t *testing.T) {
	ctx := context.Background()
	w, op, clk := newTestWatch(sessionOpen{ch: make(chan api.SessionStatsData)}, sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	w.event(ctx, api.SessionStatsData{}, false, time.Now()) // an early failure: a retry is due
	w.dial(ctx, openRotation)                               // a rotation timer that had already fired
	w.opened(ctx, recv(t, w))
	if w.ch != nil || op.ctxs[1].Err() == nil {
		t.Errorf("the late rotation was kept: ch=%v", w.ch != nil)
	}
	if clk.pending() != 1 {
		t.Errorf("the retry is still due: pending=%d", clk.pending())
	}
}

// dead: nothing about the viewer can come any more on this status stream.
func TestSessionWatch_Dead(t *testing.T) {
	ctx := context.Background()
	w, _, _ := newTestWatch()
	if w.dead() {
		t.Error("not asked yet is not dead")
	}
	w.start(ctx, sessionTarget{})
	if !w.dead() {
		t.Error("the export named no thp")
	}
	w, _, _ = newTestWatch(sessionOpen{err: &api.StatusError{Status: 400}})
	w.start(ctx, testTarget)
	if w.dead() {
		t.Error("dialling is not dead")
	}
	w.opened(ctx, recv(t, w))
	if !w.dead() {
		t.Error("a final answer")
	}
	w, _, clk := newTestWatch(sessionOpen{err: &api.StatusError{Status: 502}}, sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	if w.dead() {
		t.Error("a retry is due")
	}
	clk.fire()
	w.opened(ctx, recv(t, w))
	if w.dead() {
		t.Error("a live stream")
	}
	fail := sessionOpen{err: errors.New("refused")}
	w, _, clk = newTestWatch(fail, fail, fail, fail)
	w.start(ctx, testTarget)
	for i := 0; i <= sessionRetries; i++ {
		w.opened(ctx, recv(t, w))
		clk.fire()
	}
	if !w.dead() {
		t.Error("the retries ran out")
	}
	op := &fakeOpener{}
	w = newSessionWatch("res", op.open, (&fakeClock{}).after, func() (sessionToken, error) { return sessionToken{}, errors.New("no session") })
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	if !w.dead() {
		t.Error("no token for a viewer without a session")
	}
}
