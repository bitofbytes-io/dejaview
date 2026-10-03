package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagerIssuesAndValidatesSignedCookie(t *testing.T) {
	manager := NewManager("test-secret", 90*24*time.Hour, false, 0)
	manager.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatalf("NewCookie returned error: %v", err)
	}

	if cookie.Name != CookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, CookieName)
	}
	if cookie.HttpOnly != true {
		t.Fatal("cookie should be HttpOnly")
	}
	if cookie.MaxAge != int((90 * 24 * time.Hour).Seconds()) {
		t.Fatalf("cookie max age = %d, want 90 days", cookie.MaxAge)
	}
	if !manager.Valid(cookie.Value) {
		t.Fatal("expected signed cookie to validate")
	}
}

func TestManagerRejectsTamperedCookie(t *testing.T) {
	manager := NewManager("test-secret", time.Hour, false, 0)
	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatalf("NewCookie returned error: %v", err)
	}

	tampered := strings.Replace(cookie.Value, ".", ".tampered", 1)
	if manager.Valid(tampered) {
		t.Fatal("expected tampered cookie to be rejected")
	}
}

func TestManagerRejectsExpiredCookie(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	manager := NewManager("test-secret", time.Hour, false, 0)
	manager.now = func() time.Time { return now }

	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatalf("NewCookie returned error: %v", err)
	}

	manager.now = func() time.Time { return now.Add(2 * time.Hour) }
	if manager.Valid(cookie.Value) {
		t.Fatal("expected expired cookie to be rejected")
	}
}

func TestManagerValidRequest(t *testing.T) {
	manager := NewManager("test-secret", time.Hour, false, 0)
	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatalf("NewCookie returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)

	if !manager.ValidRequest(req) {
		t.Fatal("expected request cookie to validate")
	}
}

// previousReleaseCookie builds a cookie value exactly as the release before
// session epochs did: expires.nonce, signed with HMAC-SHA256 of the token.
func previousReleaseCookie(t *testing.T, apiToken string, expiresAt time.Time) string {
	t.Helper()
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	payload := strconv.FormatInt(expiresAt.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce)
	mac := hmac.New(sha256.New, []byte(apiToken))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestSessionEpochKeepsExistingCookiesValid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	at := func(epoch int64) *Manager {
		manager := NewManager("test-secret", 90*24*time.Hour, false, epoch)
		manager.now = func() time.Time { return now }
		return manager
	}
	old := previousReleaseCookie(t, "test-secret", now.Add(30*24*time.Hour))

	// A cookie without an epoch counts as epoch 0, so the deploy signs no one out.
	if !at(0).Valid(old) {
		t.Fatal("cookie from the previous release rejected at epoch 0")
	}
	// Raising the epoch ends it.
	if at(1).Valid(old) {
		t.Fatal("cookie from the previous release accepted at epoch 1")
	}

	// Epoch 0 still issues the old format, which the previous release accepts.
	cookie, err := at(0).NewCookie()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(cookie.Value, ".") != 2 {
		t.Fatalf("epoch 0 cookie %q is not expires.nonce.signature", cookie.Value)
	}

	cookie, err = at(7).NewCookie()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 4 || parts[2] != "7" {
		t.Fatalf("epoch 7 cookie %q is not expires.nonce.7.signature", cookie.Value)
	}
	if !at(7).Valid(cookie.Value) {
		t.Fatal("epoch 7 cookie rejected at epoch 7")
	}
	for _, epoch := range []int64{0, 6, 8} {
		if at(epoch).Valid(cookie.Value) {
			t.Fatalf("epoch 7 cookie accepted at epoch %d", epoch)
		}
	}

	// The epoch is signed: re-numbering or dropping it breaks the signature.
	renumbered := strings.Join([]string{parts[0], parts[1], "8", parts[3]}, ".")
	if at(8).Valid(renumbered) {
		t.Fatal("re-numbered epoch accepted")
	}
	dropped := strings.Join([]string{parts[0], parts[1], parts[3]}, ".")
	if at(0).Valid(dropped) {
		t.Fatal("cookie with its epoch removed accepted at epoch 0")
	}
	// Each epoch has one encoding.
	for _, epoch := range []string{"0", "07", "+7", "-7", "x"} {
		forged := strings.Join([]string{parts[0], parts[1], epoch}, ".")
		mac := hmac.New(sha256.New, []byte("test-secret"))
		mac.Write([]byte(forged))
		value := forged + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		for _, manager := range []*Manager{at(0), at(7)} {
			if manager.Valid(value) {
				t.Fatalf("epoch %q accepted at epoch %d", epoch, manager.epoch)
			}
		}
	}
}
