package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	CookieName = "dejaview_session"
	NonceSize  = 32
)

// Manager creates and validates signed browser session cookies.
type Manager struct {
	signingKey    []byte
	ttl           time.Duration
	secureCookies bool
	now           func() time.Time
}

// NewManager returns a session manager backed by the API token as an HMAC key.
func NewManager(apiToken string, ttl time.Duration, secureCookies bool) *Manager {
	return &Manager{
		signingKey:    []byte(apiToken),
		ttl:           ttl,
		secureCookies: secureCookies,
		now:           time.Now,
	}
}

// NewCookie creates a signed session cookie containing an expiry timestamp and random nonce.
func (m *Manager) NewCookie() (*http.Cookie, error) {
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate session nonce: %w", err)
	}

	expiresAt := m.now().Add(m.ttl)
	payload := strconv.FormatInt(expiresAt.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce)
	signature := m.sign(payload)

	return &http.Cookie{
		Name:     CookieName,
		Value:    payload + "." + signature,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// ClearCookie returns a cookie that clears the browser session.
func (m *Manager) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}

// ValidRequest reports whether the request has a valid signed session cookie.
func (m *Manager) ValidRequest(r *http.Request) bool {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return m.Valid(cookie.Value)
}

// Valid reports whether a signed session value is authentic and unexpired.
func (m *Manager) Valid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}

	expiresAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if !m.now().Before(time.Unix(expiresAt, 0)) {
		return false
	}

	payload := parts[0] + "." + parts[1]
	wantSignature := m.sign(payload)
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(wantSignature)) == 1
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
