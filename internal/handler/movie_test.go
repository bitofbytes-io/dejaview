package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/session"
)

func TestSearchTMDBRejectsOverlongQuery(t *testing.T) {
	handler := NewMovieHandler(nil, nil, nil, nil, session.NewManager("secret", time.Hour, false))

	req := httptest.NewRequest(http.MethodGet, "/api/tmdb/search?q="+strings.Repeat("a", 121), nil)
	recorder := httptest.NewRecorder()

	handler.SearchTMDB(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}
