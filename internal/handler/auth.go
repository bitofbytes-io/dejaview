package handler

import (
	"crypto/subtle"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/drywaters/dejaview/internal/auth"
	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

// AuthHandler handles authentication
type AuthHandler struct {
	apiToken       string
	sessionManager *session.Manager
	limiter        *auth.LoginLimiter
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(apiToken string, sessionManager *session.Manager, limiter *auth.LoginLimiter) *AuthHandler {
	return &AuthHandler{
		apiToken:       apiToken,
		sessionManager: sessionManager,
		limiter:        limiter,
	}
}

// LoginPage renders the login page
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated via valid cookie, redirect to home
	if h.sessionManager.ValidRequest(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	errorType := r.URL.Query().Get("error")
	redirectURL := r.URL.Query().Get("redirect")
	pages.LoginPage(errorType, redirectURL).Render(r.Context(), w)
}

// Login handles the login form submission
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		slog.Info("login failed", "reason", "invalid_request")
		http.Redirect(w, r, "/login?error=invalid_request", http.StatusSeeOther)
		return
	}

	apiKey := r.FormValue("api_key")
	if apiKey == "" {
		slog.Info("login failed", "reason", "missing_key")
		http.Redirect(w, r, "/login?error=missing_key", http.StatusSeeOther)
		return
	}

	// Limit by client IP only when it is known; see middleware.RealIP.
	clientIP, ipVerified := middleware.VerifiedClientIP(r)
	limitIP := ""
	if ipVerified {
		limitIP = clientIP
	}
	if wait, ok := h.limiter.Allow(limitIP); !ok {
		slog.Warn("login failed", "reason", "rate_limited", "client_ip", clientIP, "client_ip_verified", ipVerified)
		redirectURL := r.FormValue("redirect")
		if !isValidRedirect(redirectURL) {
			redirectURL = ""
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		w.WriteHeader(http.StatusTooManyRequests)
		pages.LoginPage("rate_limited", redirectURL).Render(r.Context(), w)
		return
	}

	// Validate the API token with constant-time comparison
	if !constantTimeEqual(apiKey, h.apiToken) {
		slog.Info("login failed", "reason", "invalid_key", "client_ip", clientIP, "client_ip_verified", ipVerified)
		http.Redirect(w, r, "/login?error=invalid_key", http.StatusSeeOther)
		return
	}
	h.limiter.Succeed(limitIP)

	cookie, err := h.sessionManager.NewCookie()
	if err != nil {
		slog.Error("session creation failed", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, cookie)

	// Redirect to the original URL if provided, otherwise home
	redirectURL := r.FormValue("redirect")
	if redirectURL == "" || !isValidRedirect(redirectURL) {
		redirectURL = "/"
	}
	slog.Info("login successful", "redirect_to", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// isValidRedirect checks that the redirect URL is safe (relative path only)
func isValidRedirect(rawURL string) bool {
	if strings.Contains(rawURL, "\\") || strings.ContainsFunc(rawURL, unicode.IsControl) {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return false
	}
	// url.Parse decodes the path. Validate that representation too, so encoded
	// slashes, backslashes and controls cannot change the destination's authority.
	path := parsed.Path
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") &&
		!strings.Contains(path, "\\") && !strings.ContainsFunc(path, unicode.IsControl)
}

// Logout clears the session cookie
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, h.sessionManager.ClearCookie())

	redirectTarget := logoutRedirectTarget(r)
	loginURL := "/login"
	if redirectTarget != "" {
		loginURL += "?redirect=" + url.QueryEscape(redirectTarget)
	}

	slog.Info("logout", "redirect_to", redirectTarget)
	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}

func logoutRedirectTarget(r *http.Request) string {
	if redirect := r.FormValue("redirect"); isValidRedirect(redirect) && redirect != "/login" {
		return redirect
	}

	if redirect := r.URL.Query().Get("redirect"); isValidRedirect(redirect) && redirect != "/login" {
		return redirect
	}

	if referer := r.Referer(); referer != "" {
		if parsed, err := url.Parse(referer); err == nil {
			// Only trust same-origin referers.
			if parsed.Host != "" && parsed.Host != r.Host {
				return ""
			}

			path := parsed.Path
			if path == "" {
				path = "/"
			}

			if isValidRedirect(path) && path != "/login" {
				return path
			}
		}
	}

	return ""
}

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
