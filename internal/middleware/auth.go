package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/drywaters/dejaview/internal/session"
)

// Auth middleware validates browser requests using signed session cookies.
func Auth(sessionManager *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicMovieReadRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			if r.Header.Get("Authorization") != "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if !sessionManager.ValidRequest(r) {
				http.SetCookie(w, sessionManager.ClearCookie())
				if shouldReturnUnauthorized(r) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				redirectToLogin(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isPublicMovieReadRequest(r *http.Request) bool {
	if !isSafeMethod(r.Method) {
		return false
	}

	return isPublicReadEndpoint(r.URL.Path)
}

func isPublicReadEndpoint(path string) bool {
	switch {
	case path == "/":
		// Explicitly allow the public movie-browsing landing page.
		return true
	case path == "/dashboard-content":
		// Explicitly allow HTMX dashboard partial updates for read-only browsing.
		return true
	case path == "/movies":
		return true
	case strings.HasPrefix(path, "/movies/"):
		return true
	default:
		return false
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func shouldReturnUnauthorized(r *http.Request) bool {
	isAPIRequest := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
	return isAPIRequest || !isSafeMethod(r.Method)
}

// redirectToLogin redirects to login page, preserving the original URL
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	originalURL := r.URL.String()

	// Only add redirect param if not going to root
	loginURL := "/login"
	if originalURL != "/" {
		loginURL = "/login?redirect=" + url.QueryEscape(originalURL)
	}

	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}
