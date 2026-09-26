package resource

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli"
	cp "github.com/webtor-io/claims-provider/proto"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/services/api"
	uclaims "github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/payments"
	"github.com/webtor-io/web-ui/services/statusview"
	vault "github.com/webtor-io/web-ui/services/vault"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// The status stream end to end: the real status handler and statusLoop
// against one fake that is both rest-api (the list and the export the loop
// asks for) and thp (the export's download URL points back at it, so the
// derived /session-stats URL does too). The helpers have their own tests;
// this is the wiring between them — that the session stream is opened only
// when asked for, with a token minted here and never sent on, and that the
// view on the SSE message follows it.

// fakeNode is rest-api and the torrent's thp in one server.
type fakeNode struct {
	srv *httptest.Server
	// events are written to a /session-stats stream in order, then the
	// stream stays open and silent until the client goes.
	events   []string
	sessions atomic.Int32
	mu       sync.Mutex
	sessURL  string
	export   string
	// sessStatus answers /session-stats with this status and no stream.
	sessStatus int
	// pace: after events, one more at-cap event every pace until the
	// token's expiry, where the stream ends -- as thp's does. Zero: silent
	// until the client goes.
	pace time.Duration
	// openDelay: every /session-stats answers this late (a slow node).
	openDelay time.Duration
	// stats: the seeder's stats stream -- when set, the export carries a
	// torrent_client_stat item (the content is not cached) and its URL
	// serves these frames at their times, then stays open and silent --
	// or, with statsEnd, ends (the seeder closes it once the torrent is
	// complete); statsEndedAt is when.
	stats        []statFrame
	statsEnd     bool
	statsEndedAt time.Time
	// statsNext: what every stats stream after the first serves instead
	// of stats (a reconnect lands on a new pod); nil -- the same frames.
	// statOpens counts the stats streams opened.
	statsNext []statFrame
	statOpens atomic.Int32
	// sessFrames: the /session-stats stream writes these at their times
	// from its open (after events), then stays open and silent;
	// sessWritten is when each went out.
	sessFrames  []statFrame
	sessWritten []time.Time
}

// statFrame is one statupdate frame of the seeder's stats stream, written
// at its time from the stream's open.
type statFrame struct {
	at   time.Duration
	data string
}

func newFakeNode(t *testing.T, events []string, opts ...func(*fakeNode)) *fakeNode {
	t.Helper()
	n := &fakeNode{events: events}
	for _, o := range opts {
		o(n)
	}
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/resource/"+ssHash+"/list":
			_ = json.NewEncoder(w).Encode(ra.ListResponse{ListItem: ra.ListItem{ID: "root", Size: 1 << 30}})
		case r.URL.Path == "/resource/"+ssHash+"/export/root":
			n.mu.Lock()
			n.export = r.URL.RawQuery
			n.mu.Unlock()
			// Cached content: no torrent_client_stat, the download item
			// stands in for the node's host.
			items := map[string]ra.ExportItem{
				"download": {URL: n.srv.URL + "/" + ssHash + "/Movie.mkv?api-key=K&download=true&token=EXPORT-TOKEN"},
			}
			if n.stats != nil {
				items["torrent_client_stat"] = ra.ExportItem{URL: n.srv.URL + "/" + ssHash + "/stat?stats=true"}
			}
			_ = json.NewEncoder(w).Encode(ra.ExportResponse{ExportItems: items})
		case r.URL.Path == "/"+ssHash+"/stat" && n.stats != nil:
			w.Header().Set("Content-Type", "text/event-stream")
			frames := n.stats
			if n.statOpens.Add(1) > 1 && n.statsNext != nil {
				frames = n.statsNext
			}
			start := time.Now()
			for _, f := range frames {
				select {
				case <-time.After(time.Until(start.Add(f.at))):
				case <-r.Context().Done():
					return
				}
				_, _ = fmt.Fprintf(w, "event: statupdate\ndata: %s\n\n", f.data)
				w.(http.Flusher).Flush()
			}
			if n.statsEnd {
				n.mu.Lock()
				n.statsEndedAt = time.Now()
				n.mu.Unlock()
				return
			}
			<-r.Context().Done()
		case r.URL.Path == "/session-stats/"+ssHash:
			n.sessions.Add(1)
			n.mu.Lock()
			n.sessURL = r.URL.String()
			n.mu.Unlock()
			if n.sessStatus != 0 {
				w.WriteHeader(n.sessStatus)
				return
			}
			if n.openDelay > 0 {
				select {
				case <-time.After(n.openDelay):
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, e := range n.events {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
				w.(http.Flusher).Flush()
			}
			start := time.Now()
			for _, f := range n.sessFrames {
				select {
				case <-time.After(time.Until(start.Add(f.at))):
				case <-r.Context().Done():
					return
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", f.data)
				w.(http.Flusher).Flush()
				n.mu.Lock()
				n.sessWritten = append(n.sessWritten, time.Now())
				n.mu.Unlock()
			}
			if n.pace == 0 {
				<-r.Context().Done()
				return
			}
			tok, _, err := jwt.NewParser().ParseUnverified(r.URL.Query().Get("token"), jwt.MapClaims{})
			if err != nil {
				t.Errorf("session-stats token: %v", err)
				return
			}
			exp, _ := tok.Claims.GetExpirationTime()
			end := time.NewTimer(time.Until(exp.Time))
			defer end.Stop()
			tick := time.NewTicker(n.pace)
			defer tick.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-end.C:
					return
				case <-tick.C:
					_, _ = fmt.Fprintf(w, "data: %s\n\n", atCapEvent)
					w.(http.Flusher).Flush()
				}
			}
		default:
			t.Errorf("unexpected upstream call %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *fakeNode) sessionURL() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.sessURL
}

// sessFrameAt is when the i-th of sessFrames went out; zero before it did.
func (n *fakeNode) sessFrameAt(i int) time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	if i < len(n.sessWritten) {
		return n.sessWritten[i]
	}
	return time.Time{}
}

func (n *fakeNode) statsEnded() time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.statsEndedAt
}

func (n *fakeNode) exportQuery() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.export
}

// testAPI points an api.Api at the fake with every flag explicit: the flags
// read their env vars on Apply, and a developer shell with RAPIDAPI_HOST set
// would otherwise send these requests to the real API.
func testAPI(t *testing.T, upstream *httptest.Server) *api.Api {
	t.Helper()
	u, _ := url.Parse(upstream.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{
		"--webtor-rest-api-host", host,
		"--webtor-rest-api-port", port,
		"--webtor-rest-api-secure=false",
		"--webtor-secret", testSecret,
		"--rapidapi-host", "",
		"--rapidapi-key", "",
	}); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), upstream.Client())
}

type catalogSource struct{ c *payments.Catalog }

func (s catalogSource) Catalog(context.Context) (*payments.Catalog, error) { return s.c, nil }

// liveOffers is the production storefront of 2026-09-24: silver is the promo
// plan with a trial, gold (100) the fastest plan on sale.
func liveOffers() *offer.Service {
	rate := func(v int64) *int64 { return &v }
	s := offer.New(catalogSource{&payments.Catalog{
		Prices: []payments.Price{
			{TierID: 1, TierName: "bronze", PeriodDays: 365},
			{TierID: 2, TierName: "silver", PeriodDays: 30, TrialDays: 7, IsPromo: true},
			{TierID: 3, TierName: "gold", PeriodDays: 30},
		},
		Tiers: []payments.Tier{
			{TierID: 0, Name: "free", DownloadRate: rate(5)},
			{TierID: 1, Name: "bronze", DownloadRate: rate(20)},
			{TierID: 2, Name: "silver", DownloadRate: rate(50)},
			{TierID: 3, Name: "gold", DownloadRate: rate(100)},
		},
	}}, func(tier string, period int, trial bool) string { return "https://pay.example/" + tier })
	s.Refresh(context.Background())
	return s
}

// statusServer serves the real status handler the way the app does, minus
// the page: a session and a CSRF token in place, the viewer's tier in the
// claims, language routing.
func statusServer(t *testing.T, h *Handler, tier, rate string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-secret"))))
	r.Use(func(c *gin.Context) {
		c.Set("csrfSecret", "s")
		c.Set("csrfToken", "tok")
		ctx := context.WithValue(c.Request.Context(), api.ClaimsContext{}, &api.Claims{SessionID: "s1", Domain: "webtor.example", Rate: rate, Role: tier})
		ctx = context.WithValue(ctx, uclaims.Context{}, &cp.GetResponse{Context: &cp.Context{Tier: &cp.Tier{Name: tier}}})
		c.Request = c.Request.WithContext(ctx)
	})
	r.Use(i18n.GinMiddleware(i18n.New(os.DirFS("../../locales"))))
	r.GET("/:resource_id/status", h.status)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// sseStream reads a status stream: "message" events on msgs, and every raw
// line on raw (to check what never goes out). msgs closes when the server
// ends the stream.
func sseStream(ctx context.Context, t *testing.T, u string) (msgs <-chan map[string]any, raw *strings.Builder, rawMu *sync.Mutex) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	out := make(chan map[string]any, 64)
	raw, rawMu = &strings.Builder{}, &sync.Mutex{}
	go func() {
		defer close(out)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		ev := ""
		for sc.Scan() {
			line := sc.Text()
			rawMu.Lock()
			raw.WriteString(line + "\n")
			rawMu.Unlock()
			if strings.HasPrefix(line, "event:") {
				ev = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if ev == "message" && strings.HasPrefix(line, "data:") {
				var m map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &m) == nil {
					out <- m
				}
			}
		}
	}()
	return out, raw, rawMu
}

// until reads messages until one satisfies ok, or fails after d.
func until(t *testing.T, msgs <-chan map[string]any, d time.Duration, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case m, open := <-msgs:
			if !open {
				t.Fatalf("stream ended before %s", what)
			}
			if ok(m) {
				return m
			}
		case <-deadline:
			t.Fatalf("no message with %s within %v", what, d)
		}
	}
}

// get walks a decoded JSON value: get(m, "view", "segs", 1, "tone").
func get(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// atCapEvent is thp's event for a session held at a 5M cap.
const atCapEvent = `{"window_sec":5,"bytes_per_sec":655360,"conns":1,"rate":"5M","throttled":0.8}`

// atCapEvents are thp's events for a session held at a 5M cap; the first
// one, like thp's own, covers a zero-length window, the next four a window
// thp's ring is still filling (nothing of the plan turns on over those:
// statusview.PlanBoxFromOpen of them reach the box).
func atCapEvents(n int) []string {
	out := []string{`{"window_sec":5,"bytes_per_sec":0,"conns":1,"rate":"5M"}`}
	for i := 0; i < n; i++ {
		out = append(out, atCapEvent)
	}
	return out
}

// fakeStatusVault is Vault's word on the torrent for the status loop; calls
// counts the database reads.
type fakeStatusVault struct {
	res   *vaultModels.Resource
	calls *atomic.Int32
}

func (v fakeStatusVault) GetResource(context.Context, string) (*vaultModels.Resource, error) {
	if v.calls != nil {
		v.calls.Add(1)
	}
	return v.res, nil
}

func (v fakeStatusVault) GetVaultAPIResource(context.Context, string) (*vault.Resource, error) {
	return nil, nil
}

func TestStatusStream_ViewEndToEnd(t *testing.T) {
	node := newFakeNode(t, atCapEvents(statusview.PlanBoxFromOpen))
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	msgs, raw, rawMu := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	first := until(t, msgs, 5*time.Second, "the first view", func(m map[string]any) bool { return m["view"] != nil })
	if get(first, "view", "key") != statusview.KeyCached || get(first, "view", "segs", 1, "show") != false {
		t.Errorf("before any reading: cached, the viewer not drawn: %v", first["view"])
	}
	m := until(t, msgs, 5*time.Second, "the plan box", func(m map[string]any) bool {
		return get(m, "view", "key") == statusview.KeyCachedTier && get(m, "view", "plan", "download", "box") != nil
	})
	if get(m, "state") != "cached" || get(m, "view", "segs", 1, "tone") != "plan" || get(m, "view", "segs", 1, "speed") != "5 Mbps" {
		t.Errorf("view: %v", m["view"])
	}
	cta := get(m, "view", "plan", "download", "box", "cta")
	if get(cta, "url") != "/trial?from=status-bar" || get(cta, "target") != "trial" || get(cta, "label") != "Download 10× faster" {
		t.Errorf("download cta: %v", cta)
	}
	if get(m, "view", "plan", "stream", "box", "cta", "label") != "Watch without the speed cap" {
		t.Errorf("stream cta: %v", get(m, "view", "plan", "stream"))
	}
	if sub, _ := get(m, "view", "plan", "download", "box", "sub").(string); !strings.HasPrefix(sub, "The whole file is already with us. 1.0 GB") {
		t.Errorf("the ETA prices the torrent's size: %q", sub)
	}
	// The export was asked for standard-domain URLs: the premium edge
	// buffers an event stream.
	if q, _ := url.ParseQuery(node.exportQuery()); q.Get("use-premium-domain") != "false" {
		t.Errorf("export query %q", node.exportQuery())
	}
	// The stream went to the host of the export's download URL, with the
	// export's api-key and a token minted here — not the export's own.
	su, _ := url.Parse(node.sessionURL())
	q := su.Query()
	if su.Path != "/session-stats/"+ssHash || q.Get("api-key") != "K" || q.Get("token") == "" || q.Get("token") == "EXPORT-TOKEN" || len(q) != 2 {
		t.Fatalf("session-stats request: %q", node.sessionURL())
	}
	claims := parseToken(t, q.Get("token"))
	if claims["sessionID"] != "s1" || claims["domain"] != "webtor.example" || claims["hash"] != ssHash {
		t.Errorf("token claims: %v", claims)
	}
	// thp goes quiet (the fake keeps the connection open): after
	// StaleAfter the viewer's link leaves the chain, and the plan with it.
	start := time.Now()
	m = until(t, msgs, statusview.StaleAfter+3*time.Second, "the viewer dropped", func(m map[string]any) bool {
		return get(m, "view", "segs", 1, "show") == false
	})
	if waited := time.Since(start); waited < statusview.StaleAfter-time.Second {
		t.Errorf("dropped after %v, before the stale window", waited)
	}
	if get(m, "view", "key") != statusview.KeyCached || get(m, "view", "plan") != nil {
		t.Errorf("after the drop: %v", m["view"])
	}
	if n := node.sessions.Load(); n != 1 {
		t.Errorf("session-stats opened %d times", n)
	}
	// The token never reaches the browser.
	rawMu.Lock()
	sent := raw.String()
	rawMu.Unlock()
	if strings.Contains(sent, q.Get("token")) || strings.Contains(sent, "token=") || strings.Contains(sent, "EXPORT-TOKEN") {
		t.Error("a token went out on the status stream")
	}
}

// A top-tier viewer at the cap gets the fact and no box: nothing faster is
// on sale.
func TestStatusStream_TopTierGetsNoBox(t *testing.T) {
	events := []string{`{"window_sec":5,"bytes_per_sec":0,"conns":1,"rate":"100M"}`}
	for i := 0; i < statusview.PlanBoxFromOpen; i++ {
		events = append(events, `{"window_sec":5,"bytes_per_sec":13107200,"conns":1,"rate":"100M","throttled":0.8}`)
	}
	node := newFakeNode(t, events)
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "gold", "100M")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	m := until(t, msgs, 5*time.Second, "the plan's line", func(m map[string]any) bool { return get(m, "view", "plan", "download", "hint") != nil })
	if get(m, "view", "plan", "download", "box") != nil || get(m, "view", "plan", "download", "hint") != "Download speed is capped at 100 Mbps" {
		t.Errorf("gold: %v", get(m, "view", "plan"))
	}
}

// The Vault dashboard's rows open the same endpoint without session=1 and
// draw only the state: no view, and no thp stream is opened for them.
func TestStatusStream_NoSessionStreamUnlessAsked(t *testing.T) {
	node := newFakeNode(t, atCapEvents(3))
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok")
	m := until(t, msgs, 5*time.Second, "the first status", func(m map[string]any) bool { return m["state"] == "cached" })
	if m["view"] != nil || m["label"] != "Cached" {
		t.Errorf("dashboard message: %v", m)
	}
	// The first status is out; a session stream would be dialled now.
	time.Sleep(1500 * time.Millisecond)
	if n := node.sessions.Load(); n != 0 {
		t.Errorf("session-stats opened %d times for a stream that did not ask", n)
	}
}

// "vaulted" ends the Vault dashboard's stream — its rows close on it — but
// not the resource page's: vaulted content is served through thp, and the
// viewer's link keeps changing.
func TestStatusStream_VaultedEndsOnlyTheDashboardStream(t *testing.T) {
	h := &Handler{offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	for _, c := range []struct {
		query string
		ends  bool
	}{
		{"", true},
		{"&session=1", false},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/"+ssHash+"/status?_csrf=tok&debug_status=vaulted"+c.query, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := io.ReadAll(res.Body); done <- err }()
		select {
		case <-done:
			if !c.ends {
				t.Errorf("%q: the page's stream ended on vaulted", c.query)
			}
		case <-time.After(1500 * time.Millisecond):
			if c.ends {
				t.Errorf("%q: the dashboard's stream stayed open on vaulted", c.query)
			}
		}
		cancel()
		_ = res.Body.Close()
	}
	if !endsStream(&TorrentStatus{State: "vaulted"}, &viewEnv{}) || endsStream(&TorrentStatus{State: "vaulted"}, &viewEnv{withView: true}) || endsStream(&TorrentStatus{State: "cached"}, &viewEnv{}) {
		t.Error("endsStream")
	}
	if !endsStream(&TorrentStatus{State: "vaulted", Final: true}, &viewEnv{withView: true}) {
		t.Error("a final status ends the page's stream too")
	}
}

// The debug preview goes through the same presentation as a live status:
// the plan box of a free viewer at the cap, a sample offer standing in for
// a missing catalog.
func TestDebugStatus_Preview(t *testing.T) {
	h := &Handler{} // no catalog
	srv := statusServer(t, h, "free", "5M")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1&debug_status=caching&progress=61&seeders=31&rate=4980736&plan_limited=1&bitrate=8")
	m := until(t, msgs, 3*time.Second, "the preview", func(m map[string]any) bool { return m["view"] != nil })
	if get(m, "view", "key") != statusview.KeyTier || get(m, "view", "plan", "download", "box", "cta", "url") != "/trial?from=status-bar" {
		t.Errorf("preview: %v", m["view"])
	}
	if get(m, "view", "plan", "stream", "box", "sub") != "Without a subscription — up to 5 Mbps, and this file needs 8 Mbps" {
		t.Errorf("stall sub: %v", get(m, "view", "plan", "stream", "box", "sub"))
	}
	// The cap's first seconds: the pink link, nothing sold yet.
	msgs, _, _ = sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1&debug_status=cached&plan_limited=fact")
	m = until(t, msgs, 3*time.Second, "the preview", func(m map[string]any) bool { return m["view"] != nil })
	if get(m, "view", "key") != statusview.KeyCachedTier || get(m, "view", "segs", 1, "tone") != "plan" ||
		get(m, "view", "plan", "download", "box") != nil || get(m, "view", "plan", "download", "hint") != nil {
		t.Errorf("the fact's preview: %v", m["view"])
	}
	// Without a viewer param the viewer's link is not drawn.
	msgs, _, _ = sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1&debug_status=cached")
	m = until(t, msgs, 3*time.Second, "the preview", func(m map[string]any) bool { return m["view"] != nil })
	if get(m, "view", "nodes", 2, "show") != false {
		t.Errorf("no reading asked for, yet drawn: %v", m["view"])
	}
}

// thp ends each /session-stats stream at its token's expiry. Seen from the
// page, nothing happens: the next stream is opened before that and takes
// over, and the plan box -- the only offer on the page -- stays put. (It
// used to vanish for fifteen seconds every ten minutes.) The node here is
// slow to open a stream, slower than the reading may go stale: reopening
// only once the old stream has ended would drop the viewer's link and the
// plan with it; opening the next one ahead does not.
func TestStatusStream_TokenRotationKeepsThePlan(t *testing.T) {
	ttl, lead := sessionTokenTTL, sessionRotateLead
	sessionTokenTTL, sessionRotateLead = 10*time.Second, 5*time.Second
	t.Cleanup(func() { sessionTokenTTL, sessionRotateLead = ttl, lead })
	slow := statusview.StaleAfter + 200*time.Millisecond
	node := newFakeNode(t, atCapEvents(statusview.PlanBoxFromOpen), func(n *fakeNode) {
		n.pace, n.openDelay = 250*time.Millisecond, slow
	})
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
	until(t, msgs, 10*time.Second, "the plan box", func(m map[string]any) bool {
		return get(m, "view", "key") == statusview.KeyCachedTier && get(m, "view", "plan", "download", "box") != nil
	})
	from := node.sessions.Load()
	deadline := time.After(13 * time.Second)
	for done := false; !done; {
		select {
		case m, open := <-msgs:
			if !open {
				t.Fatal("the status stream ended")
			}
			if get(m, "view", "key") != statusview.KeyCachedTier || get(m, "view", "segs", 1, "show") != true || get(m, "view", "plan", "download", "box") == nil {
				t.Fatalf("after %d rotations: %v", node.sessions.Load()-from, m["view"])
			}
		case <-deadline:
			done = true
		}
	}
	if n := node.sessions.Load() - from; n < 1 {
		t.Errorf("%d rotations in 13 s of 10 s tokens", n)
	}
}

// A vaulted torrent's page stream stays open for the viewer's own link --
// and ends, with a message the page closes on, once that link cannot come:
// otherwise it polled the Vault database every two seconds for as long as
// the tab stayed open, saying nothing new.
func TestStatusStream_VaultedEndsWhenTheViewerCannotBeFollowed(t *testing.T) {
	for _, c := range []struct {
		name string
		opt  func(*fakeNode)
		ends bool
	}{
		{"thp answers 400", func(n *fakeNode) { n.sessStatus = http.StatusBadRequest }, true},
		{"thp streams", func(*fakeNode) {}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			node := newFakeNode(t, atCapEvents(3), c.opt)
			vaulted := fakeStatusVault{res: &vaultModels.Resource{Funded: true, Vaulted: true}, calls: &atomic.Int32{}}
			h := &Handler{api: testAPI(t, node.srv), offers: liveOffers(), statusVault: vaulted}
			srv := statusServer(t, h, "free", "5M")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")
			if !c.ends {
				m := until(t, msgs, 5*time.Second, "the viewer's link", func(m map[string]any) bool {
					return get(m, "view", "segs", 1, "show") == true
				})
				if m["state"] != "vaulted" || m["final"] != nil {
					t.Errorf("vaulted with a live link: %v", m)
				}
				// Open for the viewer's link: vaulted is final on this page,
				// and the database is not asked every two seconds for it.
				from := vaulted.calls.Load()
				deadline := time.After(5 * time.Second)
				for waiting := true; waiting; {
					select {
					case _, open := <-msgs:
						if !open {
							t.Fatal("the stream ended while the viewer's link lives")
						}
					case <-deadline:
						waiting = false
					}
				}
				if n := vaulted.calls.Load() - from; n > 1 {
					t.Errorf("%d Vault reads in 5 s of a vaulted torrent", n)
				}
				return
			}
			m := until(t, msgs, 5*time.Second, "the final message", func(m map[string]any) bool { return m["final"] == true })
			if m["state"] != "vaulted" {
				t.Errorf("final: %v", m)
			}
			select {
			case _, open := <-msgs:
				if open {
					t.Error("a message after the final one")
				}
			case <-time.After(3 * time.Second):
				t.Error("the stream did not end after the final message")
			}
		})
	}
}
