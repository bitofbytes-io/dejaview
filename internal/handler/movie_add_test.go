package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/metrics"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/google/uuid"
)

func addErrorStatus(t *testing.T, err error) int {
	t.Helper()
	rec := httptest.NewRecorder()
	writeAddError(rec, err)
	return rec.Code
}

func TestParseAddRequest(t *testing.T) {
	t.Run("valid with state version", func(t *testing.T) {
		h := &MovieHandler{entryRepo: &stubMovieEntryRepo{state: model.EntryGroupState{Version: 7}}}
		req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", strings.NewReader(url.Values{"tmdb_id": {"348"}, "group_number": {"4"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		got, err := h.parseAddRequest(httptest.NewRecorder(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := (addRequest{tmdbID: 348, groupNumber: 4, groupVersion: 7}); got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	for _, tc := range []struct {
		name       string
		form       url.Values
		entries    *stubMovieEntryRepo
		wantStatus int
	}{
		{"invalid tmdb id", url.Values{"tmdb_id": {"abc"}, "group_number": {"1"}}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"missing group", url.Values{"tmdb_id": {"1"}}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"non-positive group", url.Values{"tmdb_id": {"1"}, "group_number": {"0"}}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"group state failure", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubMovieEntryRepo{stateErr: errors.New("db down")}, http.StatusInternalServerError},
		{"invalid group version", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}, "group_version": {"-1"}}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"stale group version", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}, "group_version": {"0"}}, &stubMovieEntryRepo{state: model.EntryGroupState{Version: 1}}, http.StatusConflict},
		{"stale group", url.Values{"tmdb_id": {"1"}, "group_number": {"99"}}, &stubMovieEntryRepo{checkErr: repository.ErrInvalidGroup}, http.StatusBadRequest},
		{"group check failure", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubMovieEntryRepo{checkErr: errors.New("db down")}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &MovieHandler{entryRepo: tc.entries}
			req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			_, err := h.parseAddRequest(httptest.NewRecorder(), req)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := addErrorStatus(t, err); got != tc.wantStatus {
				t.Fatalf("status %d, want %d", got, tc.wantStatus)
			}
		})
	}

	t.Run("oversized body", func(t *testing.T) {
		h := &MovieHandler{entryRepo: &stubMovieEntryRepo{}}
		req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", strings.NewReader("tmdb_id="+strings.Repeat("1", maxRequestBodyBytes)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := h.parseAddRequest(httptest.NewRecorder(), req)
		if got := addErrorStatus(t, err); got != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d, want %d (err %v)", got, http.StatusRequestEntityTooLarge, err)
		}
	})

	t.Run("malformed form", func(t *testing.T) {
		h := &MovieHandler{entryRepo: &stubMovieEntryRepo{}}
		req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", strings.NewReader("%zz"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := h.parseAddRequest(httptest.NewRecorder(), req)
		if got := addErrorStatus(t, err); got != http.StatusBadRequest {
			t.Fatalf("status %d, want %d (err %v)", got, http.StatusBadRequest, err)
		}
	})
}

func TestResolveMovie(t *testing.T) {
	t.Run("reuses existing movie without TMDB", func(t *testing.T) {
		existing := &model.Movie{ID: uuid.New()}
		client := &stubTMDBClient{}
		h := &MovieHandler{movieRepo: &stubMovieRepo{existing: existing}, tmdbClient: client}
		movie, reused, err := h.resolveMovie(t.Context(), 348)
		if err != nil || movie != existing || !reused || client.getCalls != 0 {
			t.Fatalf("movie=%v reused=%v err=%v calls=%d", movie, reused, err, client.getCalls)
		}
	})

	t.Run("creates missing movie", func(t *testing.T) {
		movies := &stubMovieRepo{}
		h := &MovieHandler{movieRepo: movies, tmdbClient: &stubTMDBClient{details: &tmdb.MovieDetails{ID: 348, Title: "Alien"}}}
		movie, reused, err := h.resolveMovie(t.Context(), 348)
		if err != nil || movie == nil || reused || len(movies.created) != 1 {
			t.Fatalf("movie=%v reused=%v err=%v creates=%d", movie, reused, err, len(movies.created))
		}
	})

	for _, tc := range []struct {
		name       string
		movies     *stubMovieRepo
		client     *stubTMDBClient
		wantStatus int
	}{
		{"lookup failure", &stubMovieRepo{getErr: errors.New("db down")}, &stubTMDBClient{}, http.StatusInternalServerError},
		{"tmdb failure", &stubMovieRepo{}, &stubTMDBClient{err: errors.New("down")}, http.StatusInternalServerError},
		{"tmdb not found", &stubMovieRepo{}, &stubTMDBClient{}, http.StatusNotFound},
		{"create failure", &stubMovieRepo{createErr: errors.New("db down")}, &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &MovieHandler{movieRepo: tc.movies, tmdbClient: tc.client}
			movie, _, err := h.resolveMovie(t.Context(), 1)
			if err == nil || movie != nil {
				t.Fatalf("movie=%v err=%v, want error", movie, err)
			}
			if got := addErrorStatus(t, err); got != tc.wantStatus {
				t.Fatalf("status %d, want %d", got, tc.wantStatus)
			}
		})
	}
}

func TestInsertEntry(t *testing.T) {
	movie := &model.Movie{ID: uuid.New()}
	req := addRequest{tmdbID: 348, groupNumber: 2, groupVersion: 5}

	t.Run("success", func(t *testing.T) {
		m := metrics.New()
		entries := &stubMovieEntryRepo{}
		h := &MovieHandler{entryRepo: entries, metrics: m}
		entry, err := h.insertEntry(t.Context(), movie, req)
		if err != nil || entry == nil {
			t.Fatalf("entry=%v err=%v", entry, err)
		}
		if len(entries.created) != 1 || entries.created[0].MovieID != movie.ID || entries.created[0].GroupNumber != 2 ||
			entries.created[0].GroupVersion == nil || *entries.created[0].GroupVersion != 5 {
			t.Fatalf("unexpected entry input: %+v", entries.created)
		}
		assertDomainMetrics(t, m, "dejaview_entries_added_total", map[string]float64{`{result="success"}`: 1})
	})

	for _, tc := range []struct {
		name        string
		createErr   error
		wantStatus  int
		wantTrigger string
		wantMetric  string
	}{
		{"duplicate", repository.ErrEntryExistsInGroup, http.StatusConflict, "Already in Group 2", "failure"},
		{"invalid group", repository.ErrInvalidGroup, http.StatusBadRequest, "refreshGroups", "failure"},
		{"group numbering changed", repository.ErrGroupNumberingChanged, http.StatusConflict, "refreshGroups", "failure"},
		{"database failure", errors.New("db down"), http.StatusInternalServerError, "", "failure"},
		{"unknown commit", errors.Join(repository.ErrCommitOutcomeUnknown, errors.New("lost")), http.StatusInternalServerError, "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := metrics.New()
			h := &MovieHandler{entryRepo: &stubMovieEntryRepo{createErr: tc.createErr}, metrics: m}
			entry, err := h.insertEntry(t.Context(), movie, req)
			if err == nil || entry != nil {
				t.Fatalf("entry=%v err=%v, want error", entry, err)
			}
			rec := httptest.NewRecorder()
			writeAddError(rec, err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, tc.wantTrigger) || strings.Contains(trigger, "Movie added!") {
				t.Fatalf("trigger %q, want it to contain %q", trigger, tc.wantTrigger)
			}
			assertDomainMetrics(t, m, "dejaview_entries_added_total", map[string]float64{`{result="` + tc.wantMetric + `"}`: 1})
		})
	}
}

func TestWriteAddErrorFallsBackToInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAddError(rec, errors.New("unclassified"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
