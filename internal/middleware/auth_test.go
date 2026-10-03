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

func TestSameOriginAllowsRefererFallback(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://dejaview.example/api/tmdb/add", nil)
	req.Header.Set("Referer", "https://dejaview.example/movies")
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestSameOriginRejectsCrossSiteReferer(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://dejaview.example/api/tmdb/add", nil)
	req.Header.Set("Referer", "https://evil.example/movies")
	recorder := httptest.NewRecorder()

	next.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}

func TestSameOriginIgnoresInvalidForwardedProto(t *testing.T) {
	next := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://dejaview.example/api/tmdb/add", nil)
	req.Header.Set("X-Forwarded-Proto", "gopher")
	req.Header.Set("Origin", "http://dejaview.example")
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

func TestAuth_SetsAuthenticatedFlag(t *testing.T) {
	manager := testSessionManager(t)
	cases := []struct {
		name       string
		method     string
		path       string
		withCookie bool
		want       bool
	}{
		{"public read without cookie", http.MethodGet, "/movies/123", false, false},
		{"public read with cookie", http.MethodGet, "/", true, true},
		{"protected write with cookie", http.MethodPost, "/api/tmdb/add", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got, called bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				got = IsAuthenticated(r.Context())
			})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.withCookie {
				req.AddCookie(testSessionCookie(t, manager))
			}
			Auth(manager)(next).ServeHTTP(httptest.NewRecorder(), req)
			if !called || got != tc.want {
				t.Fatalf("handler called=%v, IsAuthenticated=%v, want %v", called, got, tc.want)
			}
		})
	}
	if IsAuthenticated(httptest.NewRequest(http.MethodGet, "/", nil).Context()) {
		t.Fatal("request that never passed Auth reported as authenticated")
	}
}

func TestAuth_MoviesIndexIsNotPublic(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unauthenticated /movies reached the handler")
	})
	recorder := httptest.NewRecorder()
	Auth(testSessionManager(t))(next).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/movies", nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/login?redirect=%2Fmovies" {
		t.Fatalf("status %d location %q, want redirect to login", recorder.Code, recorder.Header().Get("Location"))
	}
}
