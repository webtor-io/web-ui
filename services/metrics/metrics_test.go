package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
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
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]dto.MetricType{
		"webui_http_requests_total":           dto.MetricType_COUNTER,
		"webui_http_request_duration_seconds": dto.MetricType_HISTOGRAM,
		"webui_http_requests_in_flight":       dto.MetricType_GAUGE,
		"webui_panics_total":                  dto.MetricType_COUNTER,
		"webui_jobs_total":                    dto.MetricType_COUNTER,
		"webui_jobs_in_flight":                dto.MetricType_GAUGE,
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
