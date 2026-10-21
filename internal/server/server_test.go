package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/config"
	"github.com/drywaters/dejaview/internal/metrics"
	"github.com/drywaters/dejaview/internal/session"
)

// TestPeopleRoutesRequireLogin checks that nobody can read or change people
// without a session.
func TestPeopleRoutesRequireLogin(t *testing.T) {
	router := New(&config.Config{APIToken: "api-token"}, nil, nil, nil, nil, nil, nil, nil, nil).Router()
	id := "0b0a8f5e-2d86-4a46-8f0c-5e0d3f2f6f11"
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/people", http.StatusSeeOther},
		{http.MethodPost, "/people", http.StatusUnauthorized},
		{http.MethodPost, "/people/" + id, http.StatusUnauthorized},
		{http.MethodPost, "/people/" + id + "/delete", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Origin", "http://example.com")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != tc.want {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.path, recorder.Code, tc.want)
		}
	}
}

// TestRouterRecordsMetrics sends requests through the real router and checks
// the metrics listener's handler sees route patterns and login failures, and
// that the public router does not serve /metrics.
func TestRouterRecordsMetrics(t *testing.T) {
	m := metrics.New()
	db := pingerFunc(func(context.Context) error { return nil })
	router := New(&config.Config{APIToken: "api-token"}, db, nil, nil, nil, nil, nil, nil, m).Router()

	id := "0b0a8f5e-2d86-4a46-8f0c-5e0d3f2f6f11"
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"api_key": {"wrong"}}.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Origin", "http://example.com")
	updatePerson := httptest.NewRequest(http.MethodPost, "/people/"+id, nil)
	updatePerson.Header.Set("Origin", "http://example.com")
	for _, tc := range []struct {
		req  *http.Request
		want int
	}{
		// Movie pages are public; the nil repository panics, which Recoverer
		// turns into a 500 that the metrics middleware still records.
		{httptest.NewRequest(http.MethodGet, "/movies/"+id, nil), http.StatusInternalServerError},
		{updatePerson, http.StatusUnauthorized},
		{httptest.NewRequest(http.MethodGet, "/metrics", nil), http.StatusNotFound},
		{httptest.NewRequest(http.MethodGet, "/no-such-page", nil), http.StatusNotFound},
		{httptest.NewRequest(http.MethodGet, "/health", nil), http.StatusOK},
		{login, http.StatusSeeOther},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, tc.req)
		if recorder.Code != tc.want {
			t.Fatalf("%s %s: status %d, want %d", tc.req.Method, tc.req.URL.Path, recorder.Code, tc.want)
		}
	}

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/movies/{id}",status="500"} 1`,
		`http_requests_total{method="POST",route="/people/{id}",status="401"} 1`,
		`http_requests_total{method="POST",route="/login",status="303"} 1`,
		`http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`dejaview_login_failures_total{reason="invalid_key"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	for _, unwanted := range []string{id, "/no-such-page", `route="/health"`, `route="/metrics"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("metrics contain %q", unwanted)
		}
	}
}

type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// TestHealthRoutesSkipLogin checks that probes reach /health and /ready
// without a session.
func TestHealthRoutesSkipLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pingErr error
		path    string
		want    int
	}{
		{"health", nil, "/health", http.StatusOK},
		{"health with database down", errors.New("down"), "/health", http.StatusOK},
		{"ready", nil, "/ready", http.StatusOK},
		{"ready with database down", errors.New("down"), "/ready", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := pingerFunc(func(context.Context) error { return tc.pingErr })
			router := New(&config.Config{APIToken: "api-token"}, db, nil, nil, nil, nil, nil, nil, nil).Router()
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != tc.want {
				t.Fatalf("GET %s: status %d, want %d", tc.path, recorder.Code, tc.want)
			}
		})
	}
}

// TestPreviousReleaseSessionStaysValid sends a cookie built exactly as the
// release before session epochs did through the real router: it still signs
// in at the default epoch 0, and raising SESSION_EPOCH ends it.
func TestPreviousReleaseSessionStaysValid(t *testing.T) {
	payload := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, session.NonceSize))
	mac := hmac.New(sha256.New, []byte("api-token"))
	mac.Write([]byte(payload))
	cookie := &http.Cookie{Name: session.CookieName, Value: payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}

	for _, tc := range []struct {
		epoch int64
		want  int
	}{
		// The handler rejects the entry ID only after Auth let the request in.
		{0, http.StatusBadRequest},
		{1, http.StatusUnauthorized},
	} {
		router := New(&config.Config{APIToken: "api-token", SessionEpoch: tc.epoch}, nil, nil, nil, nil, nil, nil, nil, nil).Router()
		req := httptest.NewRequest(http.MethodDelete, "/api/entries/not-a-uuid", nil)
		req.Header.Set("Origin", "http://example.com")
		req.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != tc.want {
			t.Fatalf("epoch %d: status %d, want %d", tc.epoch, recorder.Code, tc.want)
		}
	}
}

func TestRatingsPutRequiresLogin(t *testing.T) {
	router := New(&config.Config{APIToken: "api-token"}, nil, nil, nil, nil, nil, nil, nil, nil).Router()
	req := httptest.NewRequest(http.MethodPut, "/api/entries/0b0a8f5e-2d86-4a46-8f0c-5e0d3f2f6f11/ratings", strings.NewReader(`{"ratings":[]}`))
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out PUT ratings: status %d, want 401", recorder.Code)
	}
}
