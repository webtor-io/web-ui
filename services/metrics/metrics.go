// Package metrics holds the service's Prometheus instrumentation. Everything
// is registered on the default registry, which common-services exposes on the
// metrics port (see cs.NewProm in serve.go).
//
// Every label here is drawn from a set fixed at build time: route templates,
// HTTP methods, status codes, queue names. Nothing that arrives from a request
// (paths with hashes, query values, user ids) may become a label — each new
// value would be a new series kept for the life of the process.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/webtor-io/web-ui/services/offer"
)

const namespace = "webui"

// Job outcomes. A stoplist rejection is a block working as intended, not a
// failure: it gets its own outcome so an error-rate alert does not fire on a
// spike of blocked hashes.
const (
	JobOK       = "ok"
	JobError    = "error"
	JobRejected = "rejected"
)

// routeUnmatched labels requests no route claimed (404s, probes for
// /wp-login.php and the like). One series instead of one per probed path.
const routeUnmatched = "unmatched"

// methodOther collapses methods outside the set the router can answer. The
// method string is client-controlled, so it cannot be a label as-is.
const methodOther = "other"

// streamingKey marks a request whose handler holds the response open (SSE,
// long poll) — see Streaming.
const streamingKey = "webui.metrics.streaming"

// durationBuckets stop at 30 s: a page render past that is an outage, and
// the held-open routes are kept out of the histogram altogether.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// knownMethods bounds the method label: the standard verbs plus the WebDAV
// set the /webdav and /s3 mounts accept.
var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true, http.MethodTrace: true,
	http.MethodConnect: true,
	"PROPFIND":         true, "PROPPATCH": true, "MKCOL": true, "COPY": true, "MOVE": true, "LOCK": true, "UNLOCK": true,
}

// set is one complete family of collectors bound to a registry. The service
// uses the package-level one on the default registry; tests build their own so
// each runs against an empty registry instead of reading through what the
// previous test left behind.
type set struct {
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	inFlight  prometheus.Gauge
	panics    *prometheus.CounterVec
	jobs      *prometheus.CounterVec
	jobsInFly prometheus.Gauge
	paywall   *prometheus.CounterVec
	trial     *prometheus.CounterVec
}

func newSet(r prometheus.Registerer) *set {
	f := promauto.With(r)
	return &set{
		requests: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "http", Name: "requests_total",
			Help: "HTTP requests answered, by route template, method and status code.",
		}, []string{"route", "method", "status"}),
		duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "http", Name: "request_duration_seconds",
			Help:    "Time to answer a request, by route template and method. Routes that hold the response open (SSE) are not observed.",
			Buckets: durationBuckets,
		}, []string{"route", "method"}),
		inFlight: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "http", Name: "requests_in_flight",
			Help: "HTTP requests currently being handled, held-open ones included.",
		}),
		panics: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "panics_total",
			Help: "Handler panics recovered by the router, by route template.",
		}, []string{"route"}),
		jobs: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "jobs_total",
			Help: "Async job runs finished, by queue and outcome (ok, error, rejected). Replays of a stored result are not runs.",
		}, []string{"job", "outcome"}),
		jobsInFly: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "jobs_in_flight",
			Help: "Async job scripts currently executing.",
		}),
		paywall: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "stremio", Name: "paywall_video_total",
			Help: "Stremio playback clicks answered with the paywall clip instead of a stream, by clip language and method (HEAD is Stremio's pre-play probe).",
		}, []string{"lang", "method"}),
		trial: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "trial_shortlink_total",
			Help: "Visits to the /trial short link, by where it sent them (checkout, donate, none = nothing on sale), campaign (paywall, none, other) and the site surface the link sat on (from: offer.TrialFroms, none, other).",
		}, []string{"target", "campaign", "from"}),
	}
}

var std = newSet(prometheus.DefaultRegisterer)

// Middleware records every request the engine handles. It must sit outside
// the recovery middleware: a status is only known once the handler chain has
// returned, and a panicking chain returns through recovery, which writes the
// 500. Placed inside it, the middleware would unwind first and count the
// request as whatever the default status was.
func Middleware() gin.HandlerFunc {
	return std.middleware()
}

func (s *set) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		s.inFlight.Inc()
		defer s.inFlight.Dec()
		start := time.Now()
		c.Next()
		route, method := RouteLabel(c), methodLabel(c.Request.Method)
		s.requests.WithLabelValues(route, method, strconv.Itoa(c.Writer.Status())).Inc()
		if !c.GetBool(streamingKey) {
			s.duration.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
		}
	}
}

// Streaming goes first in the handler chain of a route whose response stays
// open for as long as the client listens (the job log, the status badge, AI
// recommendations over SSE). Such a request's wall time measures the viewer's
// patience, not the server, and would sit in the top bucket and pin every
// percentile there. The route is still counted in requests_total and in the
// in-flight gauge.
//
// The mark is on the route definition, not derived from response headers, so
// it cannot be missed by a handler that sets the content type late or fails
// before setting it.
func Streaming(c *gin.Context) {
	c.Set(streamingKey, true)
}

// RouteLabel is the request's route as the router matched it — the template
// with parameter names, never the concrete path.
func RouteLabel(c *gin.Context) string {
	if p := c.FullPath(); p != "" {
		return p
	}
	return routeUnmatched
}

func methodLabel(m string) string {
	if knownMethods[m] {
		return m
	}
	return methodOther
}

// PanicRecovered is called from the router's recovery handler.
func PanicRecovered(c *gin.Context) {
	std.panicRecovered(c)
}

func (s *set) panicRecovered(c *gin.Context) {
	s.panics.WithLabelValues(RouteLabel(c)).Inc()
}

// JobStarted / JobFinished bracket one execution of a job script. queue is
// the job queue name, a fixed set chosen in code (load, enrich, the action
// names); the job id, which carries the infohash, must not be passed here.
func JobStarted() {
	std.jobsInFly.Inc()
}

func JobFinished(queue, outcome string) {
	std.jobsInFly.Dec()
	std.jobs.WithLabelValues(queue, outcome).Inc()
}

// StremioPaywallVideo counts one playback click answered with the paywall
// clip. lang is the clip's language — one of the locale codes a clip was
// rendered for, never the account's raw setting — and method is the request
// method, bounded like the HTTP series.
func StremioPaywallVideo(lang, method string) {
	std.paywall.WithLabelValues(lang, methodLabel(method)).Inc()
}

// Trial short-link targets and campaigns. Both are closed sets: the campaign
// is read from a client-supplied utm_campaign, so anything but the one the
// paywall clip prints collapses into "other".
const (
	TrialTargetCheckout = "checkout"
	TrialTargetDonate   = "donate"
	TrialTargetNone     = "none"
)

// TrialShortlink counts one visit to /trial. utmCampaign and from are the
// raw query values; both are bounded here — from to a surface a link on the
// site names (offer.TrialFroms), "none" without one, "other" for the rest.
func TrialShortlink(target, utmCampaign, from string) {
	std.trial.WithLabelValues(target, campaignLabel(utmCampaign), offer.TrialFromLabel(from)).Inc()
}

func campaignLabel(c string) string {
	switch c {
	case "":
		return "none"
	case "paywall":
		return "paywall"
	}
	return "other"
}
