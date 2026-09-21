package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/services/metrics/metricstest"
)

// A handler panic must come back as a 500 AND land in logrus with the
// request that caused it.
func TestRecoverToLog(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.CustomRecovery(RecoverToLog))
	r.GET("/boom", func(c *gin.Context) { panic("kaboom") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom?x=1", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	e := hook.LastEntry()
	if e == nil || e.Level != log.ErrorLevel || e.Message != "panic recovered" {
		t.Fatalf("expected logrus error 'panic recovered', got %+v", e)
	}
	if e.Data["panic"] != "kaboom" || e.Data["path"] != "/boom" || e.Data["query"] != "x=1" {
		t.Errorf("fields = %+v", e.Data)
	}
}

// The panic counter is what an alert watches; the log line alone was not
// enough on 2026-09-18 (see RecoverToLog). The label is the route template,
// not the concrete path.
func TestRecoverToLog_CountsPanicUnderRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const route = "/recovery-test/:id"
	labels := map[string]string{"route": route}
	before := metricstest.Counter(t, "webui_panics_total", labels)

	r := gin.New()
	r.Use(gin.CustomRecovery(RecoverToLog))
	r.GET(route, func(c *gin.Context) { panic("handler exploded") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/recovery-test/abc", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := metricstest.Counter(t, "webui_panics_total", labels) - before; got != 1 {
		t.Fatalf("panics_total{route=%q} = %v, want 1", route, got)
	}
}
