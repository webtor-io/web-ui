package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/webtor-io/web-ui/services/offer"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newEngine builds a router the way serve.go does, minus the logger: the
// metrics middleware outside recovery, a page route, an SSE route marked
// Streaming, and a route that panics.
func newEngine(s *set) *gin.Engine {
	r := gin.New()
	r.Use(s.middleware(), gin.CustomRecovery(func(c *gin.Context, _ any) {
		s.panicRecovered(c)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	r.GET("/page/:id", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.GET("/missing/:id", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})
	r.GET("/events/:id", Streaming, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Stream(func(w io.Writer) bool {
			c.SSEvent("message", "one")
			return false
		})
	})
	r.GET("/boom", func(c *gin.Context) {
		panic("handler exploded")
	})
	return r
}

// recorder adds CloseNotify, which gin's c.Stream asserts on the writer and
// httptest's recorder does not implement.
type recorder struct {
	*httptest.ResponseRecorder
}

func (recorder) CloseNotify() <-chan bool { return make(chan bool) }

func do(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := recorder{httptest.NewRecorder()}
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.ResponseRecorder
}

// histogram returns the sample count of the duration series for route/method,
// and false when no such series exists.
func histogram(t *testing.T, reg *prometheus.Registry, route, method string) (uint64, bool) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "webui_http_request_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, p := range m.GetLabel() {
				labels[p.GetName()] = p.GetValue()
			}
			if labels["route"] == route && labels["method"] == method {
				return m.GetHistogram().GetSampleCount(), true
			}
		}
	}
	return 0, false
}

func TestMiddleware_CountsByRouteTemplateAndStatus(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	r := newEngine(s)

	do(r, http.MethodGet, "/page/abc")
	do(r, http.MethodGet, "/page/def")
	do(r, http.MethodGet, "/missing/x")
	do(r, http.MethodGet, "/nowhere")

	if got := testutil.ToFloat64(s.requests.WithLabelValues("/page/:id", "GET", "200")); got != 2 {
		t.Fatalf("two hits on the template must share one series: got %v", got)
	}
	if got := testutil.ToFloat64(s.requests.WithLabelValues("/missing/:id", "GET", "404")); got != 1 {
		t.Fatalf("status label must be the code the handler wrote: got %v", got)
	}
	if got := testutil.ToFloat64(s.requests.WithLabelValues("unmatched", "GET", "404")); got != 1 {
		t.Fatalf("a path no route claims must land in the unmatched series: got %v", got)
	}
	if got := testutil.CollectAndCount(s.requests); got != 3 {
		t.Fatalf("concrete paths must not create series: got %d series, want 3", got)
	}
}

func TestMiddleware_UnknownMethodIsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	r := newEngine(s)

	do(r, "FOOBAR", "/page/abc")
	do(r, "QUX", "/page/abc")

	// The router has no tree for a made-up method, so the route is unmatched
	// too; what matters is that neither method string became a label.
	if got := testutil.ToFloat64(s.requests.WithLabelValues("unmatched", "other", "404")); got != 2 {
		t.Fatalf("client-chosen methods must collapse into one series: got %v", got)
	}
	if got := testutil.CollectAndCount(s.requests); got != 1 {
		t.Fatalf("got %d series, want 1", got)
	}
}

func TestMiddleware_DurationSkipsStreamingRoutes(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	r := newEngine(s)

	do(r, http.MethodGet, "/page/abc")
	w := do(r, http.MethodGet, "/events/abc")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE route must still answer: %d %q", w.Code, w.Header().Get("Content-Type"))
	}

	if n, ok := histogram(t, reg, "/page/:id", "GET"); !ok || n != 1 {
		t.Fatalf("page route must be observed once: ok=%v n=%d", ok, n)
	}
	if _, ok := histogram(t, reg, "/events/:id", "GET"); ok {
		t.Fatal("a Streaming route must not create a duration series")
	}
	if got := testutil.ToFloat64(s.requests.WithLabelValues("/events/:id", "GET", "200")); got != 1 {
		t.Fatalf("a Streaming route is still counted in requests_total: got %v", got)
	}
}

func TestMiddleware_InFlightReturnsToZero(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	r := gin.New()
	r.Use(s.middleware(), gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	var seen float64
	r.GET("/", func(c *gin.Context) {
		seen = testutil.ToFloat64(s.inFlight)
	})
	r.GET("/boom", func(c *gin.Context) { panic("x") })

	do(r, http.MethodGet, "/")
	if seen != 1 {
		t.Fatalf("gauge must be 1 while the handler runs: got %v", seen)
	}
	do(r, http.MethodGet, "/boom")
	if got := testutil.ToFloat64(s.inFlight); got != 0 {
		t.Fatalf("gauge must return to 0 after a normal and a panicking request: got %v", got)
	}
}

func TestMiddleware_PanicIsCountedAs500(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	r := newEngine(s)

	if w := do(r, http.MethodGet, "/boom"); w.Code != http.StatusInternalServerError {
		t.Fatalf("recovery must answer 500: got %d", w.Code)
	}
	if got := testutil.ToFloat64(s.panics.WithLabelValues("/boom")); got != 1 {
		t.Fatalf("panic must be counted under its route: got %v", got)
	}
	// The request itself lands in requests_total with the status recovery
	// wrote — this is what fixes the middleware's place outside recovery.
	if got := testutil.ToFloat64(s.requests.WithLabelValues("/boom", "GET", "500")); got != 1 {
		t.Fatalf("panicking request must be counted as a 500: got %v", got)
	}
}

func TestJobCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	old := std
	std = s
	defer func() { std = old }()

	JobStarted()
	if got := testutil.ToFloat64(s.jobsInFly); got != 1 {
		t.Fatalf("in-flight after start: got %v", got)
	}
	JobFinished("load", JobRejected)
	if got := testutil.ToFloat64(s.jobsInFly); got != 0 {
		t.Fatalf("in-flight after finish: got %v", got)
	}
	if got := testutil.ToFloat64(s.jobs.WithLabelValues("load", JobRejected)); got != 1 {
		t.Fatalf("outcome series: got %v", got)
	}
}

// Every collector registers under the webui namespace: a dashboard built on
// these names must not break on a rename.
func TestMetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	s.requests.WithLabelValues("/", "GET", "200")
	s.duration.WithLabelValues("/", "GET")
	s.panics.WithLabelValues("/")
	s.jobs.WithLabelValues("load", JobOK)
	s.caps.WithLabelValues("unknown")
	s.capChecks.WithLabelValues("failed")
	s.fallback.WithLabelValues("user", "unknown")
	s.vodRoute.WithLabelValues("eac3")
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]dto.MetricType{
		"webui_http_requests_total":                dto.MetricType_COUNTER,
		"webui_http_request_duration_seconds":      dto.MetricType_HISTOGRAM,
		"webui_http_requests_in_flight":            dto.MetricType_GAUGE,
		"webui_panics_total":                       dto.MetricType_COUNTER,
		"webui_jobs_total":                         dto.MetricType_COUNTER,
		"webui_jobs_in_flight":                     dto.MetricType_GAUGE,
		"webui_transcoder_capability":              dto.MetricType_GAUGE,
		"webui_transcoder_capability_checks_total": dto.MetricType_COUNTER,
		"webui_passthrough_fallback_total":         dto.MetricType_COUNTER,
		"webui_vod_reroute_total":                  dto.MetricType_COUNTER,
		"webui_trial_shortlink_total":              dto.MetricType_COUNTER,
	}
	for _, f := range families {
		typ, ok := want[f.GetName()]
		if !ok {
			t.Errorf("unexpected metric %s", f.GetName())
			continue
		}
		if f.GetType() != typ {
			t.Errorf("%s: type %v, want %v", f.GetName(), f.GetType(), typ)
		}
		delete(want, f.GetName())
	}
	for name := range want {
		t.Errorf("metric %s missing", name)
	}
}

// The paywall and trial counters take their labels from a request (the
// method, a client-supplied utm_campaign): both must stay a closed set.
func TestPaywallAndTrialLabelsAreBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	old := std
	std = s
	defer func() { std = old }()
	// The series made up front (TestTrialSeriesExistBeforeTheFirstClick)
	// are not what is counted here.
	s.trial.Reset()

	StremioPaywallVideo("ru", "GET")
	StremioPaywallVideo("ru", "BREW")
	TrialShortlink(TrialTargetCheckout, "paywall", "")
	TrialShortlink(TrialTargetCheckout, "", "")
	TrialShortlink(TrialTargetCheckout, "spring-sale", "")
	TrialShortlink(TrialTargetCheckout, "../../etc", "")

	if got := testutil.ToFloat64(s.paywall.WithLabelValues("ru", "other")); got != 1 {
		t.Errorf("an unknown method must collapse into other: got %v", got)
	}
	for campaign, want := range map[string]float64{"paywall": 1, "none": 1, "other": 2} {
		if got := testutil.ToFloat64(s.trial.WithLabelValues(TrialTargetCheckout, campaign, "none")); got != want {
			t.Errorf("campaign %q: got %v, want %v", campaign, got, want)
		}
	}
	if got := testutil.CollectAndCount(s.trial); got != 3 {
		t.Errorf("client-chosen campaigns made %d series, want 3", got)
	}

	// from is client-supplied too: a surface of offer.TrialFroms keeps its
	// name, the rest — "none" typed in by hand included — is "other".
	s.trial.Reset()
	for _, from := range offer.TrialFroms {
		TrialShortlink(TrialTargetCheckout, "", from)
	}
	for _, from := range []string{"reddit", "GRACE", "grace ", "none", "other", "../../etc", strings.Repeat("x", 4096)} {
		TrialShortlink(TrialTargetCheckout, "", from)
	}
	TrialShortlink(TrialTargetCheckout, "", "")
	for _, from := range offer.TrialFroms {
		if got := testutil.ToFloat64(s.trial.WithLabelValues(TrialTargetCheckout, "none", from)); got != 1 {
			t.Errorf("from %q: got %v, want 1", from, got)
		}
	}
	if got := testutil.ToFloat64(s.trial.WithLabelValues(TrialTargetCheckout, "none", "other")); got != 7 {
		t.Errorf("unknown from values: got %v in other, want 7", got)
	}
	if got := testutil.ToFloat64(s.trial.WithLabelValues(TrialTargetCheckout, "none", "none")); got != 1 {
		t.Errorf("no from: got %v in none, want 1", got)
	}
	if got, want := testutil.CollectAndCount(s.trial), len(offer.TrialFroms)+2; got != want {
		t.Errorf("client-chosen from values made %d series, want %d", got, want)
	}
}

// A counter series that first appears at 1 has no earlier sample, and
// increase() over a window that starts before it counts nothing for that
// first click: every restart lost the first click of each surface (the
// player's label read 46 against 79 in the log over a week). The series a
// click on the site can make exist at 0 from the start.
func TestTrialSeriesExistBeforeTheFirstClick(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	old := std
	std = s
	defer func() { std = old }()
	froms := append([]string{offer.TrialFromNone}, offer.TrialFroms...)
	for _, target := range []string{TrialTargetCheckout, TrialTargetDonate} {
		for _, from := range froms {
			if !hasSeries(t, reg, "webui_trial_shortlink_total", map[string]string{"target": target, "campaign": "none", "from": from}) {
				t.Errorf("no series for %s from %s before its first click", target, from)
			}
		}
	}
	if got, want := testutil.CollectAndCount(s.trial), 2*len(froms); got != want {
		t.Errorf("%d series up front, want %d", got, want)
	}
	TrialShortlink(TrialTargetCheckout, "", offer.FromPlayerLabel)
	if got := testutil.ToFloat64(s.trial.WithLabelValues(TrialTargetCheckout, "none", offer.FromPlayerLabel)); got != 1 {
		t.Errorf("the first click: %v", got)
	}
}

// hasSeries: the registry holds a series of name with exactly these labels.
func hasSeries(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) bool {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			got := map[string]string{}
			for _, p := range m.GetLabel() {
				got[p.GetName()] = p.GetValue()
			}
			if len(got) == len(labels) {
				same := true
				for k, v := range labels {
					same = same && got[k] == v
				}
				if same {
					return true
				}
			}
		}
	}
	return false
}

// The capability gauge is one-hot over a closed set of answers, and the
// checks counter over a closed set of results: a value from elsewhere cannot
// make a series.
func TestTranscoderCapabilityIsOneHotAndBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	old := std
	std = s
	defer func() { std = old }()

	TranscoderCapability("on")
	for a, want := range map[string]float64{"on": 1, "off": 0, "unknown": 0} {
		if got := testutil.ToFloat64(s.caps.WithLabelValues(a)); got != want {
			t.Errorf("after on: %s = %v, want %v", a, got, want)
		}
	}
	TranscoderCapability("off")
	TranscoderCapability("maybe")
	for a, want := range map[string]float64{"on": 0, "off": 0, "unknown": 1} {
		if got := testutil.ToFloat64(s.caps.WithLabelValues(a)); got != want {
			t.Errorf("after an unknown value: %s = %v, want %v", a, got, want)
		}
	}
	if got := testutil.CollectAndCount(s.caps); got != 3 {
		t.Errorf("%d gauge series, want 3", got)
	}
	for _, r := range []string{"on", "off", "failed", "timeout", "../x"} {
		TranscoderCapabilityCheck(r)
	}
	if got := testutil.ToFloat64(s.capChecks.WithLabelValues("failed")); got != 3 {
		t.Errorf("failed = %v, want 3 (every non-answer is failed)", got)
	}
	if got := testutil.CollectAndCount(s.capChecks); got != 3 {
		t.Errorf("%d check series, want 3", got)
	}
}

// The fallback counter's labels come from a form field: a reason or a class
// outside the closed sets is one series, not one per value.
func TestPassthroughFallbackIsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	old := std
	std = s
	defer func() { std = old }()

	PassthroughFallback("decode_error", "hevc10-2160")
	PassthroughFallback("decode_error", "hevc10-2160")
	PassthroughFallback("user", "hevc8")
	PassthroughFallback("<script>", "../x")
	PassthroughFallback("", "")
	// The audio classes (multichannel audio) are series of their own.
	PassthroughFallback("decode_error", "dolby")
	PassthroughFallback("media_error", "aac51")
	// A fragment loaded again and again (web-ui fragment-loop.js).
	PassthroughFallback("fragment_loop", "hevc10")
	if got := testutil.ToFloat64(s.fallback.WithLabelValues("decode_error", "hevc10-2160")); got != 2 {
		t.Errorf("decode_error/hevc10-2160 = %v, want 2", got)
	}
	if got := testutil.ToFloat64(s.fallback.WithLabelValues("other", "unknown")); got != 2 {
		t.Errorf("other/unknown = %v, want 2", got)
	}
	if got := testutil.ToFloat64(s.fallback.WithLabelValues("decode_error", "dolby")); got != 1 {
		t.Errorf("decode_error/dolby = %v, want 1", got)
	}
	if got := testutil.ToFloat64(s.fallback.WithLabelValues("media_error", "aac51")); got != 1 {
		t.Errorf("media_error/aac51 = %v, want 1", got)
	}
	if got := testutil.ToFloat64(s.fallback.WithLabelValues("fragment_loop", "hevc10")); got != 1 {
		t.Errorf("fragment_loop/hevc10 = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(s.fallback); got != 6 {
		t.Errorf("%d series, want 6", got)
	}
}

// The reroute counter's label is the job's reason; anything else is one
// series.
func TestVODRerouteIsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := newSet(reg)
	for _, r := range []string{"eac3_ts", "eac3", "ac3", "no_decoder", "eac3", "<x>", ""} {
		s.vodReroute(r)
	}
	if got := testutil.ToFloat64(s.vodRoute.WithLabelValues("eac3")); got != 2 {
		t.Errorf("eac3 = %v, want 2", got)
	}
	if got := testutil.ToFloat64(s.vodRoute.WithLabelValues("other")); got != 2 {
		t.Errorf("other = %v, want 2", got)
	}
	if got := testutil.CollectAndCount(s.vodRoute); got != 5 {
		t.Errorf("%d series, want 5", got)
	}
}

// Each Mark becomes a segment of the time since the previous one, in chain
// order, and mw covers all of it: a slow middleware shows as its own segment.
func TestServerTimingSegmentsByMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	slow := func(c *gin.Context) { time.Sleep(30 * time.Millisecond); c.Next() }
	r.Use(Middleware(), Mark("a"), slow, Mark("b"))
	r.GET("/", func(c *gin.Context) { c.Header("Server-Timing", ServerTiming(c)) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	parts := strings.Split(rec.Header().Get("Server-Timing"), ", ")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "a;dur=") || !strings.HasPrefix(parts[1], "b;dur=") || !strings.HasPrefix(parts[2], "mw;dur=") {
		t.Fatalf("Server-Timing %q, want a, b, mw in that order", parts)
	}
	var b float64
	if _, err := fmt.Sscanf(parts[1], "b;dur=%f", &b); err != nil || b < 30 {
		t.Fatalf("segment b = %v (%v), want the 30 ms the middleware before it slept", b, err)
	}
}
