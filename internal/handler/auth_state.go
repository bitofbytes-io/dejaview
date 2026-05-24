package handler

import (
	"net/http"

	"github.com/drywaters/dejaview/internal/session"
)

func isAuthenticatedRequest(r *http.Request, sessionManager *session.Manager) bool {
	if sessionManager == nil {
		return false
	}
	return sessionManager.ValidRequest(r)
}
