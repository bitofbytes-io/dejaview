package middleware

import "net/http"

// SecurityHeaders sets the baseline browser policy before downstream handlers
// write responses. HSTS follows the deployment's HTTPS configuration, not
// request headers, so local HTTP remains usable and proxy headers cannot opt in.
func SecurityHeaders(secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "same-origin")
			w.Header().Set("X-Frame-Options", "DENY")
			if secureCookies {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
