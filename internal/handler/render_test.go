package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/drywaters/dejaview/internal/auth"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

func TestDashboardRenderErrorPreservesPartialResponse(t *testing.T) {
	h := &DashboardHandler{entryRepo: &stubDashboardEntries{}, personRepo: &stubPersonRepo{}}
	view := func([]pages.GroupData, []*model.Person, int, pages.RatingFilter, bool) templ.Component {
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			if _, err := io.WriteString(w, "<main>partial"); err != nil {
				return err
			}
			return errors.New("template failed after writing")
		})
	}
	w := httptest.NewRecorder()
	h.render(w, httptest.NewRequest(http.MethodGet, "/", nil), view)
	if w.Code != http.StatusOK || w.Body.String() != "<main>partial" {
		t.Fatalf("render error changed partial response: status=%d body=%q", w.Code, w.Body.String())
	}
}

type failedRenderWriter struct {
	header http.Header
	status []int
	writes int
}

func (w *failedRenderWriter) Header() http.Header { return w.header }
func (w *failedRenderWriter) WriteHeader(status int) {
	w.status = append(w.status, status)
}
func (w *failedRenderWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("client disconnected")
}

func TestRateLimitedLoginRenderErrorPreservesStatus(t *testing.T) {
	limiter := auth.NewLoginLimiter(1, 1, time.Minute)
	limiter.Allow("")
	h := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), limiter, nil)
	form := url.Values{"api_key": {"secret"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := &failedRenderWriter{header: make(http.Header)}
	h.Login(w, r)
	if len(w.status) != 1 || w.status[0] != http.StatusTooManyRequests || w.writes != 1 {
		t.Fatalf("render error attempted a second response: statuses=%v writes=%d", w.status, w.writes)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response lost Retry-After")
	}
}
