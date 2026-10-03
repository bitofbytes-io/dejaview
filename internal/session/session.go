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
	epoch         int64
	now           func() time.Time
}

// NewManager returns a session manager backed by the API token as an HMAC key.
// A cookie is valid only while its epoch equals epoch, so raising epoch
// signs out every browser without changing the token.
func NewManager(apiToken string, ttl time.Duration, secureCookies bool, epoch int64) *Manager {
	return &Manager{
		signingKey:    []byte(apiToken),
		ttl:           ttl,
		secureCookies: secureCookies,
		epoch:         epoch,
		now:           time.Now,
	}
}

// NewCookie creates a signed session cookie containing an expiry timestamp,
// a random nonce and, unless it is 0, the manager's epoch.
func (m *Manager) NewCookie() (*http.Cookie, error) {
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate session nonce: %w", err)
	}

	expiresAt := m.now().Add(m.ttl)
	payload := strconv.FormatInt(expiresAt.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce)
	// Epoch 0 keeps the cookie format from before epochs existed, so the
	// previous release still accepts it during a rolling deploy.
	if m.epoch != 0 {
		payload += "." + strconv.FormatInt(m.epoch, 10)
	}
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

// Valid reports whether a signed session value is authentic, unexpired and
// from the current epoch. The value is expires.nonce.signature for epoch 0,
// which includes every cookie issued before epochs existed, and
// expires.nonce.epoch.signature otherwise.
func (m *Manager) Valid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}

	expiresAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if !m.now().Before(time.Unix(expiresAt, 0)) {
		return false
	}

	var epoch int64
	if len(parts) == 4 {
		epoch, err = strconv.ParseInt(parts[2], 10, 64)
		// Epoch 0 has only the short form, so each epoch has one encoding.
		if err != nil || epoch <= 0 || strconv.FormatInt(epoch, 10) != parts[2] {
			return false
		}
	}
	if epoch != m.epoch {
		return false
	}

	payload := strings.Join(parts[:len(parts)-1], ".")
	wantSignature := m.sign(payload)
	return subtle.ConstantTimeCompare([]byte(parts[len(parts)-1]), []byte(wantSignature)) == 1
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
