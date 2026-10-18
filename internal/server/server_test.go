package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/config"
	"github.com/drywaters/dejaview/internal/session"
)

// TestPeopleRoutesRequireLogin checks that nobody can read or change people
// without a session.
func TestPeopleRoutesRequireLogin(t *testing.T) {
	router := New(&config.Config{APIToken: "api-token"}, nil, nil, nil, nil, nil, nil, nil).Router()
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
			router := New(&config.Config{APIToken: "api-token"}, db, nil, nil, nil, nil, nil, nil).Router()
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
		router := New(&config.Config{APIToken: "api-token", SessionEpoch: tc.epoch}, nil, nil, nil, nil, nil, nil, nil).Router()
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
