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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/statusview"
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
	requests   *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	inFlight   prometheus.Gauge
	panics     *prometheus.CounterVec
	jobs       *prometheus.CounterVec
	jobsInFly  prometheus.Gauge
	paywall    *prometheus.CounterVec
	trial      *prometheus.CounterVec
	caps       *prometheus.GaugeVec
	capChecks  *prometheus.CounterVec
	fallback   *prometheus.CounterVec
	vodRoute   *prometheus.CounterVec
	statusView *prometheus.CounterVec
}

func newSet(r prometheus.Registerer) *set {
	f := promauto.With(r)
	s := &set{
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
			Help: "Async job runs finished, by queue and outcome (ok, error, rejected). Replays of a stored result are not runs; a run the job store refused before its script started is an error.",
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
		caps: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Name: "transcoder_capability",
			Help: "What content-transcoder last said about HEVC passthrough (GET /capabilities): 1 on the answer in effect (on, off, unknown = none heard since start), 0 on the others.",
		}, []string{"answer"}),
		capChecks: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "transcoder_capability_checks_total",
			Help: "Background questions to content-transcoder's GET /capabilities, by result (on, off, failed = no answer; the last answer stays).",
		}, []string{"result"}),
		fallback: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "passthrough_fallback_total",
			Help: "Stream starts that restart a file whose HEVC passthrough failed in the browser, by reason (codecs_rejected, decode_error, media_error, src_unsupported, no_frames, user, fragment_loop; vod_codecs: an nginx-vod stream the browser refused, restarted to the transcoder; other = anything else) and the decoder class the stream needed (hevc8, hevc10, hevc8-2160, hevc10-2160; unknown) -- or whose multichannel audio failed (class dolby, aac51; on any route).",
		}, []string{"reason", "class"}),
		vodRoute: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "vod_reroute_total",
			Help: "MP4 stream starts sent to the transcoder instead of nginx-vod because the browser cannot play their audio as nginx-vod serves it, by reason (eac3_ts: E-AC-3 in nginx-vod's MPEG-TS, which no hls.js plays; eac3, ac3: not declared; no_decoder: DTS; unserved_audio: audio nginx-vod serves none of; hevc: HEVC the declaration does not cover; fallback: a restart after the browser failed the file; other = anything else). jobs/scripts/vod_route.go.",
		}, []string{"reason"}),
		statusView: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "status_view_total",
			Help: "Resource page status streams that showed a state, by statusview key and mode (chain, badge; other = anything else): once per stream and pair, the first time a message carries it. Not the browser's refinement of tier (tier_dl, stream_ok, stream_stall).",
		}, []string{"key", "mode"}),
	}
	for _, k := range statusKeys {
		for _, m := range statusModes {
			s.statusView.WithLabelValues(k, m).Add(0)
		}
	}
	// A series that first appears at 1 has no earlier sample, and
	// increase() loses that first click -- of every surface, on every
	// restart (the player's label: 46 against 79 in the log over a week).
	// The ones a link on the site makes exist at 0 from the start.
	for _, target := range []string{TrialTargetCheckout, TrialTargetDonate} {
		s.trial.WithLabelValues(target, "none", offer.TrialFromNone).Add(0)
		for _, from := range offer.TrialFroms {
			s.trial.WithLabelValues(target, "none", from).Add(0)
		}
	}
	return s
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

const (
	startKey = "metrics.start"
	marksKey = "metrics.marks"
)

type mark struct {
	name string
	at   time.Duration
}

// Mark notes how long the request has been in the chain when it reaches this
// point (serve.go puts one after each middleware that goes to a store or a
// service). ServerTiming turns the notes into segments.
func Mark(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		MarkAt(c, name)
		c.Next()
	}
}

// MarkAt is Mark from inside a middleware, for the steps of one that calls
// out more than once (auth: the session, the SuperTokens lookups, the user
// row).
func MarkAt(c *gin.Context, name string) {
	if t, ok := c.Get(startKey); ok {
		marks, _ := c.Get(marksKey)
		l, _ := marks.([]mark)
		c.Set(marksKey, append(l, mark{name, time.Since(t.(time.Time))}))
	}
}

// ServerTiming is a Server-Timing value for the middlewares' share of the
// request so far, called from a handler: one segment per Mark (the time since
// the previous one) and mw for all of it since Middleware, which stands first
// in the chain. Empty without Middleware.
func ServerTiming(c *gin.Context) string {
	t, ok := c.Get(startKey)
	if !ok {
		return ""
	}
	var b strings.Builder
	var prev time.Duration
	marks, _ := c.Get(marksKey)
	l, _ := marks.([]mark)
	for _, m := range l {
		fmt.Fprintf(&b, "%s;dur=%.1f, ", m.name, ms(m.at-prev))
		prev = m.at
	}
	fmt.Fprintf(&b, "mw;dur=%.1f", ms(time.Since(t.(time.Time))))
	return b.String()
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (s *set) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		s.inFlight.Inc()
		defer s.inFlight.Dec()
		start := time.Now()
		c.Set(startKey, start)
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

// JobNotStarted counts a run the job store refused before its script
// started, as an error. Left out, a store outage showed as fewer jobs, not
// failing ones: on 2026-10-03 dragonfly-ui refused every write for two hours
// and jobs_total read 0.3% errors.
func JobNotStarted(queue string) {
	std.jobs.WithLabelValues(queue, JobError).Inc()
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

// statusKeys and statusModes are what the status view counter takes as
// they are: statusview's keys and modes. A key added there without being
// added here counts as "other" -- bounded, and plain to see.
var (
	statusKeys = []string{
		statusview.KeyActive, statusview.KeyChecking, statusview.KeyTier, statusview.KeySwarm, statusview.KeyStalled,
		statusview.KeyMissing, statusview.KeyCachingOnly, statusview.KeyCachingIdle, statusview.KeyCachedFlow,
		statusview.KeyCachedTier, statusview.KeyCached, statusview.KeyPaused, statusview.KeyNoSeed,
		statusview.KeyMissingIdle, statusview.KeyIdleTorrent, statusview.KeyUnknown, statusview.KeyVaulting,
		statusview.KeyVaultingOnly, statusview.KeyVaultingIdle, statusview.KeyVaulted, statusview.KeyVaultedTier,
		statusview.KeyVaultedIdle, statusview.KeyVaultWait, statusview.KeyVaultMissing, statusview.KeyVaultFailed,
		"other",
	}
	statusModes = []string{statusview.ModeChain, statusview.ModeBadge, "other"}
)

// StatusView counts a resource page status stream that showed key in mode
// (handlers/resource statusLoop: once per stream and pair). Both are bounded
// to statusview's sets.
func StatusView(key, mode string) {
	std.statusView.WithLabelValues(oneOf(key, statusKeys), oneOf(mode, statusModes)).Inc()
}

// oneOf is v when set lists it, "other" when not.
func oneOf(v string, set []string) string {
	for _, s := range set {
		if s == v {
			return v
		}
	}
	return "other"
}

// Transcoder capability answers (services/transcodercaps). A closed set:
// anything else is a bug and lands in "unknown" rather than in a new series.
var capabilityAnswers = []string{"on", "off", "unknown"}

// TranscoderCapability puts the gauge at 1 on answer and 0 on the others.
func TranscoderCapability(answer string) {
	std.transcoderCapability(answer)
}

func (s *set) transcoderCapability(answer string) {
	known := false
	for _, a := range capabilityAnswers {
		if a == answer {
			known = true
		}
	}
	if !known {
		answer = "unknown"
	}
	for _, a := range capabilityAnswers {
		v := 0.0
		if a == answer {
			v = 1
		}
		s.caps.WithLabelValues(a).Set(v)
	}
}

// TranscoderCapabilityCheck counts one background question: on, off, or
// failed (no answer).
func TranscoderCapabilityCheck(result string) {
	switch result {
	case "on", "off":
	default:
		result = "failed"
	}
	std.capChecks.WithLabelValues(result).Inc()
}

// Passthrough fallback reasons and decoder classes (docs/player.md,
// "Passthrough: errors and fallback"). Closed sets: the values come from a
// form field (models.ParseDecodeRequest allowlists them already; this keeps
// the series bounded whoever calls).
var (
	fallbackReasons = map[string]bool{"codecs_rejected": true, "decode_error": true, "media_error": true, "src_unsupported": true, "no_frames": true, "user": true, "fragment_loop": true, "vod_codecs": true}
	fallbackClasses = map[string]bool{"hevc8": true, "hevc10": true, "hevc8-2160": true, "hevc10-2160": true, "dolby": true, "aac51": true}
)

// PassthroughFallback counts one start that restarts a file whose
// passthrough failed in the browser.
func PassthroughFallback(reason, class string) {
	std.passthroughFallback(reason, class)
}

func (s *set) passthroughFallback(reason, class string) {
	if !fallbackReasons[reason] {
		reason = "other"
	}
	if !fallbackClasses[class] {
		class = "unknown"
	}
	s.fallback.WithLabelValues(reason, class).Inc()
}

// vodRerouteReasons: jobs/scripts/vod_route.go's reasons, a closed set.
var vodRerouteReasons = map[string]bool{"eac3_ts": true, "eac3": true, "ac3": true, "no_decoder": true, "unserved_audio": true, "hevc": true, "fallback": true}

// VODReroute counts one MP4 start sent to the transcoder instead of
// nginx-vod.
func VODReroute(reason string) {
	std.vodReroute(reason)
}

func (s *set) vodReroute(reason string) {
	if !vodRerouteReasons[reason] {
		reason = "other"
	}
	s.vodRoute.WithLabelValues(reason).Inc()
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
