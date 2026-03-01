package middleware

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

const cookieName = "dejaview_session"

// Auth middleware validates requests using either Bearer token or cookie.
// Programmatic clients (iOS Shortcuts, CLI) use Authorization: Bearer <token>.
// Browser clients use a cookie set during login.
func Auth(apiToken string, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicMovieReadRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Check Authorization header first (for programmatic access)
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
					if constantTimeEqual(token, apiToken) {
						next.ServeHTTP(w, r)
						return
					}
				}
				// Invalid bearer token
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Fall back to cookie check (for browser access)
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				if shouldReturnUnauthorized(r) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				redirectToLogin(w, r)
				return
			}

			if !constantTimeEqual(cookie.Value, apiToken) {
				// Invalid cookie, clear it and redirect
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					Secure:   secureCookies,
					SameSite: http.SameSiteLaxMode,
				})
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

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
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
