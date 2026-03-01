package handler

import (
	"net/http"
	"strings"
)

func isAuthenticatedRequest(r *http.Request, apiToken string) bool {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
			return constantTimeEqual(token, apiToken)
		}
		return false
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}

	return constantTimeEqual(cookie.Value, apiToken)
}
