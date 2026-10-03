package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubEntryStore struct {
	updateErr   error
	updateCalls int
	lastInput   model.UpdateEntryInput
	reorderErr  error
}

func (s *stubEntryStore) Update(ctx context.Context, id uuid.UUID, input model.UpdateEntryInput) error {
	s.updateCalls++
	s.lastInput = input
	return s.updateErr
}

func (s *stubEntryStore) Delete(ctx context.Context, id uuid.UUID) error { return nil }

func (s *stubEntryStore) ReorderEntries(ctx context.Context, groupNumber int, entryIDs []uuid.UUID) error {
	return s.reorderErr
}

func putEntry(handler *EntryHandler, entryID string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/entries/"+entryID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	route := chi.NewRouteContext()
	route.URLParams.Add("id", entryID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	handler.Update(recorder, req)
	return recorder
}

func TestUpdateEntryRejectsInvalidGroupNumber(t *testing.T) {
	for _, group := range []string{"abc", "1.5", "0", "-2"} {
		t.Run(group, func(t *testing.T) {
			store := &stubEntryStore{}
			handler := &EntryHandler{entryRepo: store}
			recorder := putEntry(handler, uuid.NewString(), url.Values{"group_number": {group}})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}
			if store.updateCalls != 0 {
				t.Fatalf("expected no update, got %d calls", store.updateCalls)
			}
			if recorder.Header().Get("HX-Trigger") != "" {
				t.Fatal("expected no success toast")
			}
		})
	}
}

func TestUpdateEntryMapsRepositoryErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"moved", nil, http.StatusOK},
		{"duplicate in target group", repository.ErrEntryExistsInGroup, http.StatusConflict},
		{"moved concurrently", repository.ErrEntryGroupChanged, http.StatusConflict},
		{"missing entry", repository.ErrEntryNotFound, http.StatusNotFound},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubEntryStore{updateErr: tc.err}
			handler := &EntryHandler{entryRepo: store}
			recorder := putEntry(handler, uuid.NewString(), url.Values{"group_number": {"3"}})
			if recorder.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, recorder.Code)
			}
			if store.lastInput.GroupNumber == nil || *store.lastInput.GroupNumber != 3 {
				t.Fatalf("expected group 3 to be passed, got %v", store.lastInput.GroupNumber)
			}
			hasToast := strings.Contains(recorder.Header().Get("HX-Trigger"), "Entry updated!")
			if hasToast != (tc.want == http.StatusOK) {
				t.Fatalf("success toast present=%v for status %d", hasToast, recorder.Code)
			}
		})
	}
}

func TestReorderMapsRepositoryErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"saved", nil, http.StatusOK},
		{"group changed", repository.ErrReorderMismatch, http.StatusConflict},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &EntryHandler{entryRepo: &stubEntryStore{reorderErr: tc.err}}
			body := `{"entry_ids": ["` + uuid.NewString() + `"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/groups/2/reorder", strings.NewReader(body))
			route := chi.NewRouteContext()
			route.URLParams.Add("num", "2")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
			recorder := httptest.NewRecorder()
			handler.Reorder(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, recorder.Code)
			}
		})
	}
}
