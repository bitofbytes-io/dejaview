package handler

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/auth"
	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/session"
)

func testLimiter() *auth.LoginLimiter {
	return auth.NewLoginLimiter(10, 50, 15*time.Minute)
}

func TestLoginSetsSignedSessionCookie(t *testing.T) {
	sessionManager := session.NewManager("secret", 90*24*time.Hour, false, 0)
	handler := NewAuthHandler("secret", sessionManager, testLimiter())

	form := url.Values{}
	form.Set("api_key", "secret")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	handler.Login(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, recorder.Code)
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	if cookies[0].Value == "secret" {
		t.Fatal("session cookie stored raw API token")
	}
	if !sessionManager.Valid(cookies[0].Value) {
		t.Fatal("session cookie did not validate")
	}
}

func TestLoginPageRejectsLegacyRawTokenCookie(t *testing.T) {
	sessionManager := session.NewManager("secret", 90*24*time.Hour, false, 0)
	handler := NewAuthHandler("secret", sessionManager, testLimiter())

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "secret"})
	recorder := httptest.NewRecorder()

	handler.LoginPage(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestLoginRedirectValidation(t *testing.T) {
	cases := []struct {
		destination string
		valid       bool
	}{
		{"/", true}, {"/entries/123?filter=watched#ratings", true}, {"/search?q=https%3A%2F%2Fexample.com", true},
		{"", false}, {"https://example.com", false}, {"//example.com", false}, {`/\example.com`, false},
		{"/%5cexample.com", false}, {"/%2fexample.com", false}, {"/%00bad", false}, {"/bad\npath", false},
		{"/%zz", false}, {"relative", false},
	}
	handler := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), testLimiter())
	for _, tc := range cases {
		t.Run(tc.destination, func(t *testing.T) {
			if got := isValidRedirect(tc.destination); got != tc.valid {
				t.Fatalf("valid=%v want=%v", got, tc.valid)
			}
			form := url.Values{"api_key": {"secret"}, "redirect": {tc.destination}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			result := httptest.NewRecorder()
			handler.Login(result, req)
			want := "/"
			if tc.valid {
				want = tc.destination
			}
			if result.Code != http.StatusSeeOther || result.Header().Get("Location") != want {
				t.Fatalf("status=%d redirect=%q", result.Code, result.Header().Get("Location"))
			}
		})
	}
}

func postLogin(handler http.Handler, remoteAddr, apiKey string) *httptest.ResponseRecorder {
	form := url.Values{"api_key": {apiKey}, "redirect": {"/stats"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func assertRateLimited(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", recorder.Code)
	}
	if wait, err := strconv.Atoi(recorder.Header().Get("Retry-After")); err != nil || wait <= 0 {
		t.Fatalf("Retry-After = %q, want positive seconds", recorder.Header().Get("Retry-After"))
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("rate-limited login set a cookie")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Too many failed attempts") || !strings.Contains(body, `value="/stats"`) {
		t.Fatalf("rate-limited page lacks the message or the redirect:\n%s", body)
	}
}

func TestLoginRateLimitsFailuresPerVerifiedIP(t *testing.T) {
	manager := session.NewManager("secret", time.Hour, false, 0)
	handler := NewAuthHandler("secret", manager, auth.NewLoginLimiter(3, 5, time.Hour))
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")}
	server := middleware.RealIP(proxies)(http.HandlerFunc(handler.Login))

	for i := range 3 {
		if recorder := postLogin(server, "203.0.113.1:4000", "wrong"); recorder.Header().Get("Location") != "/login?error=invalid_key" {
			t.Fatalf("failure %d: status %d location %q", i+1, recorder.Code, recorder.Header().Get("Location"))
		}
	}
	// The limit applies before the token is checked, so even the right one waits.
	assertRateLimited(t, postLogin(server, "203.0.113.1:4000", "secret"))

	// Another client, here forwarded by the trusted proxy, can still sign in.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"api_key": {"secret"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.RemoteAddr = "10.0.1.5:4000"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther || len(recorder.Result().Cookies()) != 1 {
		t.Fatalf("other client: status %d, cookies %d", recorder.Code, len(recorder.Result().Cookies()))
	}
}

func TestLoginRateLimitsAllClientsWhenIPIsUnknown(t *testing.T) {
	manager := session.NewManager("secret", time.Hour, false, 0)
	handler := NewAuthHandler("secret", manager, auth.NewLoginLimiter(3, 5, time.Hour))
	// Without trusted proxies the client IP is never verified, and a
	// forged X-Forwarded-For changes nothing.
	server := middleware.RealIP(nil)(http.HandlerFunc(handler.Login))

	for i := range 5 {
		if recorder := postLogin(server, "203.0.113."+strconv.Itoa(i+1)+":4000", "wrong"); recorder.Code != http.StatusSeeOther {
			t.Fatalf("failure %d: status %d", i+1, recorder.Code)
		}
	}
	assertRateLimited(t, postLogin(server, "198.51.100.9:4000", "secret"))
}

func TestLoginSuccessClearsFailures(t *testing.T) {
	manager := session.NewManager("secret", time.Hour, false, 0)
	handler := NewAuthHandler("secret", manager, auth.NewLoginLimiter(3, 5, time.Hour))
	server := http.HandlerFunc(handler.Login)

	for range 4 {
		postLogin(server, "203.0.113.1:4000", "wrong")
	}
	if recorder := postLogin(server, "203.0.113.1:4000", "secret"); recorder.Code != http.StatusSeeOther || len(recorder.Result().Cookies()) != 1 {
		t.Fatalf("login: status %d", recorder.Code)
	}
	for i := range 5 {
		if recorder := postLogin(server, "203.0.113.1:4000", "wrong"); recorder.Code != http.StatusSeeOther {
			t.Fatalf("failure %d after a successful login: status %d", i+1, recorder.Code)
		}
	}
}

func TestLoginWithoutKeyDoesNotCount(t *testing.T) {
	manager := session.NewManager("secret", time.Hour, false, 0)
	handler := NewAuthHandler("secret", manager, auth.NewLoginLimiter(1, 1, time.Hour))
	server := http.HandlerFunc(handler.Login)
	for range 3 {
		if recorder := postLogin(server, "203.0.113.1:4000", ""); recorder.Header().Get("Location") != "/login?error=missing_key" {
			t.Fatalf("missing key: status %d location %q", recorder.Code, recorder.Header().Get("Location"))
		}
	}
	if recorder := postLogin(server, "203.0.113.1:4000", "secret"); recorder.Code != http.StatusSeeOther || len(recorder.Result().Cookies()) != 1 {
		t.Fatalf("login after empty submissions: status %d", recorder.Code)
	}
}
