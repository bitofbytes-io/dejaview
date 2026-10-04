package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

type stubDashboardEntries struct {
	entries []*model.Entry
	err     error
}

func (s *stubDashboardEntries) ListAll(ctx context.Context) ([]*model.Entry, error) {
	return s.entries, s.err
}

func dashboardEntry(group int, title string) *model.Entry {
	return &model.Entry{ID: uuid.New(), GroupNumber: group, Movie: &model.Movie{Title: title}}
}

func TestGroupEntriesKeepsListOrder(t *testing.T) {
	entries := []*model.Entry{
		dashboardEntry(4, "Fargo"), dashboardEntry(4, "Heat"),
		dashboardEntry(2, "Dune"),
		dashboardEntry(1, "Alien"), dashboardEntry(1, "Brazil"), dashboardEntry(1, "Coco"),
	}
	groups := groupEntries(entries)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	wantSizes := map[int]int{4: 2, 2: 1, 1: 3}
	next := 0
	for i, group := range groups {
		if i > 0 && groups[i-1].Number <= group.Number {
			t.Fatalf("group %d listed after group %d", group.Number, groups[i-1].Number)
		}
		if len(group.Entries) != wantSizes[group.Number] {
			t.Fatalf("group %d has %d entries, want %d", group.Number, len(group.Entries), wantSizes[group.Number])
		}
		for _, entry := range group.Entries {
			if entry != entries[next] {
				t.Fatalf("group %d entry %q out of order", group.Number, entry.Movie.Title)
			}
			next++
		}
	}
	if groupEntries(nil) != nil {
		t.Fatal("expected no groups for no entries")
	}
}

func TestDashboardCurrentGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*model.Entry
		want    string
	}{
		{"empty", nil, `<option value="1" selected>Group 1 (new)</option>`},
		{"highest group", []*model.Entry{dashboardEntry(3, "Cube"), dashboardEntry(1, "Alien")}, `<option value="4">+ New group</option>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &DashboardHandler{entryRepo: &stubDashboardEntries{entries: tc.entries}, personRepo: &stubPersonRepo{}}
			recorder := httptest.NewRecorder()
			handler.DashboardContent(recorder, httptest.NewRequest(http.MethodGet, "/dashboard-content", nil))
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("status %d, body missing %q:\n%s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}
}

func TestDashboardReturns500WhenEntriesFail(t *testing.T) {
	handler := &DashboardHandler{entryRepo: &stubDashboardEntries{err: errors.New("db down")}, personRepo: &stubPersonRepo{}}
	for path, serve := range map[string]http.HandlerFunc{
		"/":                  handler.DashboardPage,
		"/dashboard-content": handler.DashboardContent,
	} {
		recorder := httptest.NewRecorder()
		serve(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("%s: status %d, want 500", path, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), "group-section") {
			t.Fatalf("%s: rendered groups despite the error", path)
		}
	}
}
