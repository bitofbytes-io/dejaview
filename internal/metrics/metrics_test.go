package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /metrics: status %d", recorder.Code)
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware)
	ok := func(w http.ResponseWriter, r *http.Request) {}
	r.Get("/health", ok)
	r.Get("/ready", ok)
	r.Get("/movies/{id}", ok)
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			})
		})
		r.Put("/api/entries/{id}", ok)
	})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/movies/123?user=alice", nil),
		httptest.NewRequest(http.MethodGet, "/movies/456", nil),
		httptest.NewRequest(http.MethodPut, "/api/entries/789", nil),
		httptest.NewRequest(http.MethodGet, "/no/such/path/1", nil),
		httptest.NewRequest(http.MethodGet, "/no/such/path/2", nil),
		httptest.NewRequest("BREW", "/movies/123", nil),
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/ready", nil),
	} {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	m.LoginFailure("invalid_key")

	body := scrape(t, m)
	for _, want := range []string{
		`http_requests_total{method="GET",route="/movies/{id}",status="200"} 2`,
		`http_requests_total{method="PUT",route="/api/entries/{id}",status="401"} 1`,
		`http_requests_total{method="GET",route="unmatched",status="404"} 2`,
		`http_requests_total{method="OTHER",`,
		`http_request_duration_seconds_bucket{method="GET",route="/movies/{id}",status="200",le="+Inf"} 2`,
		`http_request_duration_seconds_count{method="GET",route="/movies/{id}",status="200"} 2`,
		`dejaview_login_failures_total{reason="invalid_key"} 1`,
		"go_goroutines ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	for _, unwanted := range []string{"/movies/123", "/movies/456", "/api/entries/789", "/no/such/path", "alice", "BREW", `route="/health"`, `route="/ready"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("metrics contain %q", unwanted)
		}
	}
}

func TestHandlerServesOnlyMetrics(t *testing.T) {
	handler := New().Handler()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/metrics", http.StatusOK},
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/health", http.StatusNotFound},
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.want {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.path, recorder.Code, tc.want)
		}
	}
}

func TestNilMetricsIsNoop(t *testing.T) {
	var m *Metrics
	m.LoginFailure("invalid_key")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	recorder := httptest.NewRecorder()
	m.Middleware(next).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status %d, want %d", recorder.Code, http.StatusTeapot)
	}
}

func TestPgxPoolCollector(t *testing.T) {
	// The pool connects lazily, so no database is needed to read its stats.
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db?pool_max_conns=7")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	m := New()
	if err := m.Register(NewPgxPoolCollector(pool)); err != nil {
		t.Fatalf("register: %v", err)
	}
	body := scrape(t, m)
	for _, want := range []string{
		"pgxpool_max_conns 7",
		"pgxpool_acquired_conns 0",
		"pgxpool_idle_conns 0",
		"pgxpool_total_conns 0",
		"pgxpool_constructing_conns 0",
		"pgxpool_acquire_count_total 0",
		"pgxpool_acquire_duration_seconds_total 0",
		"pgxpool_empty_acquire_count_total 0",
		"pgxpool_empty_acquire_wait_seconds_total 0",
		"pgxpool_canceled_acquire_count_total 0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}
