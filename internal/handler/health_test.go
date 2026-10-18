package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHealthIsStatic(t *testing.T) {
	h := NewHealthHandler(pingerFunc(func(context.Context) error {
		t.Fatal("/health must not ping the database")
		return nil
	}))
	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
}

func TestReady(t *testing.T) {
	closedPool, err := pgxpool.New(context.Background(), "postgres://bob:secret-password@127.0.0.1:1/dejaview?sslmode=disable")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	closedPool.Close()

	for _, tc := range []struct {
		name     string
		db       Pinger
		wantCode int
		wantBody string
	}{
		{
			name:     "database up",
			db:       pingerFunc(func(context.Context) error { return nil }),
			wantCode: http.StatusOK,
			wantBody: `{"status":"ok","db":"ok"}`,
		},
		{
			name: "ping fails",
			db: pingerFunc(func(context.Context) error {
				return errors.New("dial tcp db.internal:5432: password authentication failed for user secret-user")
			}),
			wantCode: http.StatusServiceUnavailable,
			wantBody: `{"status":"degraded","db":"error"}`,
		},
		{
			name:     "closed pool",
			db:       closedPool,
			wantCode: http.StatusServiceUnavailable,
			wantBody: `{"status":"degraded","db":"error"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewHealthHandler(tc.db).Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			if rec.Code != tc.wantCode {
				t.Errorf("status %d, want %d", rec.Code, tc.wantCode)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.wantBody {
				t.Errorf("body %q, want %q", got, tc.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type %q, want application/json", got)
			}
		})
	}
}

func TestReadyBoundsPing(t *testing.T) {
	h := NewHealthHandler(pingerFunc(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ping context has no deadline")
		}
		if remaining := time.Until(deadline); remaining > readyTimeout {
			t.Fatalf("ping deadline %s away, want at most %s", remaining, readyTimeout)
		}
		<-ctx.Done()
		return ctx.Err()
	}))
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}
