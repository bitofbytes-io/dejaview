package handler

import (
	"crypto/subtle"
	"net/http"
	"net/url"

	"github.com/drywaters/dejaview/internal/ui/pages"
)

const cookieName = "dejaview_session"

// AuthHandler handles authentication
type AuthHandler struct {
	apiToken      string
	secureCookies bool
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(apiToken string, secureCookies bool) *AuthHandler {
	return &AuthHandler{
		apiToken:      apiToken,
		secureCookies: secureCookies,
	}
}

// LoginPage renders the login page
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated via valid cookie, redirect to home
	if cookie, err := r.Cookie(cookieName); err == nil {
		if constantTimeEqual(cookie.Value, h.apiToken) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
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

	// Set the session cookie with the token value
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.apiToken,
		Path:     "/",
		MaxAge:   90 * 24 * 60 * 60, // 90 days
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

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
	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

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
			path := parsed.Path
			if path == "" {
				path = "/"
			}
			target := path
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
			if isValidRedirect(target) && path != "/login" {
				return target
			}
		}
	}

	return ""
}

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}


