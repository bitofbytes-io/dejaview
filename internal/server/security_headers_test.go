package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/config"
)

func TestRouterSecurityHeadersCoverAllResponsePaths(t *testing.T) {
	// Static routes use paths relative to the application working directory.
	t.Chdir("../..")
	manifest, err := os.ReadFile("static/site.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	for _, secure := range []bool{false, true} {
		router := New(&config.Config{APIToken: "secret", SecureCookies: secure}, nil, nil, nil, nil, nil, nil, nil, nil).Router()
		for _, tc := range []struct {
			name, method, path, origin string
			status                     int
			body, location             string
		}{
			{"login", http.MethodGet, "/login", "", http.StatusOK, `name="api_key"`, ""},
			{"static", http.MethodGet, "/static/site.webmanifest", "", http.StatusOK, string(manifest), ""},
			{"root static", http.MethodGet, "/site.webmanifest", "", http.StatusOK, string(manifest), ""},
			{"health", http.MethodGet, "/health", "", http.StatusOK, "ok", ""},
			{"redirect", http.MethodGet, "/people", "", http.StatusSeeOther, "", "/login?redirect=%2Fpeople"},
			{"blocked origin", http.MethodPost, "/login", "https://other.example", http.StatusForbidden, "Forbidden\n", ""},
			{"unauthorized", http.MethodPost, "/people", "http://example.com", http.StatusUnauthorized, "Unauthorized\n", ""},
			{"not found", http.MethodGet, "/no-such-page", "", http.StatusNotFound, "404 page not found\n", ""},
			// The public movie route reaches the nil repository; Recoverer emits 500.
			{"recovered panic", http.MethodGet, "/movies/0b0a8f5e-2d86-4a46-8f0c-5e0d3f2f6f11", "", http.StatusInternalServerError, "", ""},
		} {
			t.Run(fmt.Sprintf("secure=%t/%s", secure, tc.name), func(t *testing.T) {
				r := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.origin != "" {
					r.Header.Set("Origin", tc.origin)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				response := w.Result()
				if response.StatusCode != tc.status || response.Header.Get("Location") != tc.location {
					t.Fatalf("status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
				}
				if !strings.Contains(w.Body.String(), tc.body) {
					t.Fatalf("response missing expected body %q: %q", tc.body, w.Body.String())
				}
				for name, want := range map[string]string{
					"X-Content-Type-Options": "nosniff",
					"Referrer-Policy":        "same-origin",
					"X-Frame-Options":        "DENY",
				} {
					if got := response.Header.Get(name); got != want {
						t.Errorf("%s=%q, want %q", name, got, want)
					}
				}
				wantHSTS := ""
				if secure {
					wantHSTS = "max-age=31536000"
				}
				if got := response.Header.Get("Strict-Transport-Security"); got != wantHSTS {
					t.Errorf("HSTS=%q, want %q", got, wantHSTS)
				}
				if tc.name == "static" && response.Header.Get("Cache-Control") != "public, max-age=86400" {
					t.Error("static cache policy changed")
				}
			})
		}
	}
}
