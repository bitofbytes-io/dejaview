package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerEmitsDebugAccessLog(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	logs := buf.String()
	if !strings.Contains(logs, "http request") {
		t.Fatalf("log output = %q, want request log", logs)
	}
	if !strings.Contains(logs, "path=/health") {
		t.Fatalf("log output = %q, want path", logs)
	}
	if !strings.Contains(logs, "status=204") {
		t.Fatalf("log output = %q, want status", logs)
	}
	if !strings.Contains(logs, "level=DEBUG") {
		t.Fatalf("log output = %q, want debug level", logs)
	}
}

func TestLoggerElevatesClientAndServerErrors(t *testing.T) {
	tests := []struct {
		name  string
		code  int
		level string
	}{
		{name: "client error", code: http.StatusNotFound, level: "level=WARN"},
		{name: "server error", code: http.StatusInternalServerError, level: "level=ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
			defer slog.SetDefault(previous)

			req := httptest.NewRequest(http.MethodGet, "/missing", nil)
			rec := httptest.NewRecorder()

			Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
			})).ServeHTTP(rec, req)

			if logs := buf.String(); !strings.Contains(logs, tt.level) {
				t.Fatalf("log output = %q, want %s", logs, tt.level)
			}
		})
	}
}
