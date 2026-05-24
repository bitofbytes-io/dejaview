package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/session"
)

func testSessionManager(t *testing.T) *session.Manager {
	t.Helper()
	return session.NewManager("secret", time.Hour, false)
}

func testSessionCookie(t *testing.T, manager *session.Manager) *http.Cookie {
	t.Helper()
	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatalf("NewCookie returned error: %v", err)
	}
	return cookie
}

func TestAuth_UnauthenticatedMovieReadAllowed(t *testing.T) {
	mw := Auth(testSessionManager(t))
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
	mw := Auth(testSessionManager(t))
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

func TestAuth_SignedCookieMovieWriteAllowed(t *testing.T) {
	manager := testSessionManager(t)
	mw := Auth(manager)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", nil)
	req.AddCookie(testSessionCookie(t, manager))
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestAuth_BearerTokenRejected(t *testing.T) {
	mw := Auth(testSessionManager(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestAuth_InvalidCookieRejectedAndCleared(t *testing.T) {
	mw := Auth(testSessionManager(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "secret"})
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("expected invalid cookie to be cleared, got %+v", cookies)
	}
}

func TestAuth_UnauthenticatedSafeMethodsAllowed(t *testing.T) {
	mw := Auth(testSessionManager(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tests := []struct {
		name   string
		method string
	}{
		{name: "head", method: http.MethodHead},
		{name: "options", method: http.MethodOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/movies/123", nil)
			recorder := httptest.NewRecorder()

			mw(next).ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
			}
		})
	}
}

func TestAuth_UnauthenticatedApiRootDenied(t *testing.T) {
	mw := Auth(testSessionManager(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	recorder := httptest.NewRecorder()

	mw(next).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSameOriginAllowsSafeMethodsWithoutOrigin(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://dejaview.example/movies/123", nil)
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestSameOriginRejectsUnsafeMethodWithoutOrigin(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://dejaview.example/api/tmdb/add", nil)
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}

func TestSameOriginAllowsForwardedHTTPSOrigin(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://dejaview.example/api/tmdb/add", nil)
	req.Host = "dejaview.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", "https://dejaview.example")
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestSameOriginRejectsCrossSiteOrigin(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://dejaview.example/api/tmdb/add", nil)
	req.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}
