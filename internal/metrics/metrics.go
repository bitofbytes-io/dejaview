// Package metrics exports Prometheus metrics on a separate internal listener.
//
// A Metrics value owns its own registry (not the global default) with the Go
// runtime and process collectors, HTTP RED metrics for the public router,
// database pool stats and dejaview's domain counters. Handler serves the
// registry at GET /metrics; it is never mounted on the public router.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// unmatchedRoute is the route label for requests that matched no route, so
// unknown paths collapse into a single series.
const unmatchedRoute = "unmatched"

// Metrics holds the registry and the collectors recorded by the app. A nil
// *Metrics is valid and records nothing.
type Metrics struct {
	registry        *prometheus.Registry
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	loginFailures   *prometheus.CounterVec
	entriesAdded    *prometheus.CounterVec
	ratingsSaved    *prometheus.CounterVec
	tmdbLookups     *prometheus.CounterVec
}

// New creates a registry with the Go runtime, process, HTTP and domain
// collectors.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests handled, by method, route pattern and status code.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency, by method, route pattern and status code.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		entriesAdded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dejaview_entries_added_total",
			Help: "Entry creation attempts, by committed success or failure.",
		}, []string{"result"}),
		ratingsSaved: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dejaview_ratings_saved_total",
			Help: "Non-empty scores submitted to rating batches, by committed success or failure.",
		}, []string{"result"}),
		tmdbLookups: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dejaview_tmdb_lookups_total",
			Help: "TMDB HTTP lookups, by operation and result.",
		}, []string{"operation", "result"}),
		loginFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dejaview_login_failures_total",
			Help: "Failed login attempts, by reason.",
		}, []string{"reason"}),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests,
		m.requestDuration,
		m.loginFailures,
		m.entriesAdded,
		m.ratingsSaved,
		m.tmdbLookups,
	)
	return m
}

// Register adds extra collectors, such as a database pool collector.
func (m *Metrics) Register(cs ...prometheus.Collector) error {
	for _, c := range cs {
		if err := m.registry.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// Handler serves the registry at GET /metrics and 404s everything else.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry}))
	return mux
}

// Middleware records http_requests_total and http_request_duration_seconds
// for a chi router. The route label is chi's route pattern (e.g.
// /movies/{id}), never the raw path. Probe and metrics paths are skipped.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/ready", "/metrics":
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			route := unmatchedRoute
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					route = pattern
				}
			}
			labels := prometheus.Labels{
				"method": normalizeMethod(r.Method),
				"route":  route,
				"status": strconv.Itoa(status),
			}
			m.requests.With(labels).Inc()
			m.requestDuration.With(labels).Observe(time.Since(start).Seconds())
		}()

		next.ServeHTTP(ww, r)
	})
}

// LoginFailure counts a failed login. reason must come from a fixed set.
func (m *Metrics) LoginFailure(reason string) {
	if m == nil {
		return
	}
	m.loginFailures.WithLabelValues(reason).Inc()
}

// normalizeMethod maps nonstandard methods to OTHER so clients can't create
// unbounded label values.
func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	}
	return "OTHER"
}

// EntryAdded records the result of one entry repository Create call.
func (m *Metrics) EntryAdded(result string) {
	if m != nil {
		m.entriesAdded.WithLabelValues(normalizeResult(result)).Inc()
	}
}

// RatingsSaved records non-nil scores after an atomic SaveBatch returns.
// Deletions and empty batches do not create a series or increment a counter.
func (m *Metrics) RatingsSaved(result string, count int) {
	if m != nil && count > 0 {
		m.ratingsSaved.WithLabelValues(normalizeResult(result)).Add(float64(count))
	}
}

// TMDBLookup records one HTTP attempt. Unknown operations are ignored so no
// caller-controlled strings can become labels; unknown results mean failure.
func (m *Metrics) TMDBLookup(operation, result string) {
	if m == nil || (operation != "search" && operation != "movie") {
		return
	}
	if operation != "movie" || result != "not_found" {
		result = normalizeResult(result)
	}
	m.tmdbLookups.WithLabelValues(operation, result).Inc()
}

func normalizeResult(result string) string {
	if result == "success" {
		return "success"
	}
	return "failure"
}
