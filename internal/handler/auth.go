package handler

import (
	"crypto/subtle"
	"net/http"
	"net/url"

	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

// AuthHandler handles authentication
type AuthHandler struct {
	apiToken       string
	sessionManager *session.Manager
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(apiToken string, sessionManager *session.Manager) *AuthHandler {
	return &AuthHandler{
		apiToken:       apiToken,
		sessionManager: sessionManager,
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
		http.Redirect(w, r, "/login?error=invalid_request", http.StatusSeeOther)
		return
	}

	apiKey := r.FormValue("api_key")
	if apiKey == "" {
		http.Redirect(w, r, "/login?error=missing_key", http.StatusSeeOther)
		return
	}

	// Validate the API token with constant-time comparison
	if !constantTimeEqual(apiKey, h.apiToken) {
		http.Redirect(w, r, "/login?error=invalid_key", http.StatusSeeOther)
		return
	}

	cookie, err := h.sessionManager.NewCookie()
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, cookie)

	// Redirect to the original URL if provided, otherwise home
	redirectURL := r.FormValue("redirect")
	if redirectURL == "" || !isValidRedirect(redirectURL) {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// isValidRedirect checks that the redirect URL is safe (relative path only)
func isValidRedirect(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Must be a relative path with no scheme or host (prevents open redirect)
	return parsed.Scheme == "" && parsed.Host == "" && len(parsed.Path) > 0 && parsed.Path[0] == '/'
}

// Logout clears the session cookie
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, h.sessionManager.ClearCookie())

	redirectTarget := logoutRedirectTarget(r)
	loginURL := "/login"
	if redirectTarget != "" {
		loginURL += "?redirect=" + url.QueryEscape(redirectTarget)
	}

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
