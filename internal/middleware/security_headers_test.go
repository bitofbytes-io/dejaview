package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersPreserveResponseAndUseDeploymentSetting(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, scheme := range []string{"http", "https"} {
			for _, forwarded := range []string{"http", "https"} {
				t.Run(fmt.Sprintf("secure=%t/%s/forwarded=%s", secure, scheme, forwarded), func(t *testing.T) {
					next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if w.Header().Get("X-Frame-Options") != "DENY" {
							t.Error("security headers were not set before the handler")
						}
						w.Header().Set("Content-Type", "text/plain")
						w.Header().Set("X-App-Header", "preserved")
						w.WriteHeader(http.StatusAccepted)
						_, _ = w.Write([]byte("accepted"))
					})
					r := httptest.NewRequest(http.MethodGet, scheme+"://example.com/", nil)
					r.Header.Set("X-Forwarded-Proto", forwarded)
					r.Header.Set("Forwarded", "proto="+forwarded)
					w := httptest.NewRecorder()
					SecurityHeaders(secure)(next).ServeHTTP(w, r)
					headers := w.Result().Header
					for name, want := range map[string]string{
						"X-Content-Type-Options": "nosniff",
						"Referrer-Policy":        "same-origin",
						"X-Frame-Options":        "DENY",
						"Content-Type":           "text/plain",
						"X-App-Header":           "preserved",
					} {
						if got := headers.Get(name); got != want {
							t.Errorf("%s=%q, want %q", name, got, want)
						}
					}
					wantHSTS := ""
					if secure {
						wantHSTS = "max-age=31536000"
					}
					if got := headers.Get("Strict-Transport-Security"); got != wantHSTS {
						t.Errorf("HSTS=%q, want %q", got, wantHSTS)
					}
					if w.Code != http.StatusAccepted || w.Body.String() != "accepted" {
						t.Fatalf("response changed: status=%d body=%q", w.Code, w.Body.String())
					}
				})
			}
		}
	}
}
