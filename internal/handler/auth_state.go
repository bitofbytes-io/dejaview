package handler

import (
	"net/http"
	"strings"
)

func isAuthenticatedRequest(r *http.Request, apiToken string) bool {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
			if apiToken != "" && constantTimeEqual(token, apiToken) {
				return true
			}
			// Explicit bearer token present but invalid.
			return false
		}
		// Non-bearer auth header: fall through and allow cookie auth to decide.
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}

	return apiToken != "" && constantTimeEqual(cookie.Value, apiToken)
}
