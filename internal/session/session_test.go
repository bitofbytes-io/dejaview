package session

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagerIssuesAndValidatesSignedCookie(t *testing.T) {
	manager := NewManager("test-secret", 90*24*time.Hour, false)
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
	manager := NewManager("test-secret", time.Hour, false)
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
	manager := NewManager("test-secret", time.Hour, false)
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
	manager := NewManager("test-secret", time.Hour, false)
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
