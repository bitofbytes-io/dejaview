package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth_UnauthenticatedMovieReadAllowed(t *testing.T) {
	mw := Auth("secret", false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/movies/123", nil)
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestAuth_UnauthenticatedMovieWriteDenied(t *testing.T) {
	mw := Auth("secret", false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", nil)
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestAuth_AuthenticatedMovieWriteAllowed(t *testing.T) {
	mw := Auth("secret", false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}
