package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// readyTimeout bounds the database ping so a hung connection fails the
// readiness check instead of stalling the caller.
const readyTimeout = 2 * time.Second

// Pinger reports whether a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves the liveness and readiness checks.
type HealthHandler struct {
	db Pinger
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

type readyResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}

// Health reports that the process is up. It checks no dependencies.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready reports whether the database answers a ping. It returns 503 when it
// does not, and never includes the error, which can name hosts or users.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	resp := readyResponse{Status: "ok", DB: "ok"}
	code := http.StatusOK
	if err := h.db.Ping(ctx); err != nil {
		slog.Warn("readiness check failed", "dependency", "db", "error", err)
		resp = readyResponse{Status: "degraded", DB: "error"}
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}
