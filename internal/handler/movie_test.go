package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

type stubMovieRepo struct {
	existing    *model.Movie
	created     []model.CreateMovieInput
	getByTMDBID []int
}

func (s *stubMovieRepo) GetByTMDBId(ctx context.Context, tmdbID int) (*model.Movie, error) {
	s.getByTMDBID = append(s.getByTMDBID, tmdbID)
	return s.existing, nil
}

func (s *stubMovieRepo) Create(ctx context.Context, input model.CreateMovieInput) (*model.Movie, error) {
	s.created = append(s.created, input)
	return &model.Movie{ID: uuid.New(), Title: input.Title, TMDBId: input.TMDBId}, nil
}

type stubMovieEntryRepo struct {
	createErr    error
	existing     *model.Entry
	created      []model.CreateEntryInput
	lookupGroups []int
}

func (s *stubMovieEntryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	return nil, nil
}

func (s *stubMovieEntryRepo) GetByMovieAndGroup(ctx context.Context, movieID uuid.UUID, groupNumber int) (*model.Entry, error) {
	s.lookupGroups = append(s.lookupGroups, groupNumber)
	return s.existing, nil
}

func (s *stubMovieEntryRepo) Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error) {
	s.created = append(s.created, input)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &model.Entry{ID: uuid.New(), MovieID: input.MovieID, GroupNumber: input.GroupNumber}, nil
}

type stubTMDBClient struct {
	details  *tmdb.MovieDetails
	err      error
	getCalls int
}

func (s *stubTMDBClient) Search(ctx context.Context, query string) (*tmdb.SearchResponse, error) {
	return &tmdb.SearchResponse{}, nil
}

func (s *stubTMDBClient) GetMovie(ctx context.Context, tmdbID int) (*tmdb.MovieDetails, error) {
	s.getCalls++
	return s.details, s.err
}

func (s *stubTMDBClient) PosterURL(path string, size string) string {
	return "https://images.test/" + size + path
}

func postAddFromTMDB(handler *MovieHandler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/tmdb/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.AddFromTMDB(recorder, req)
	return recorder
}

func TestAddFromTMDBCreatesMovieAndEntry(t *testing.T) {
	poster := "/alien.jpg"
	movies := &stubMovieRepo{}
	entries := &stubMovieEntryRepo{}
	client := &stubTMDBClient{details: &tmdb.MovieDetails{ID: 348, Title: "Alien", ReleaseDate: "1979-05-25", PosterPath: &poster, Runtime: 117}}
	handler := &MovieHandler{movieRepo: movies, entryRepo: entries, tmdbClient: client}

	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"4"}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if client.getCalls != 1 || len(movies.created) != 1 {
		t.Fatalf("expected one TMDB fetch and movie create, got fetches=%d creates=%d", client.getCalls, len(movies.created))
	}
	created := movies.created[0]
	if created.Title != "Alien" || created.TMDBId == nil || *created.TMDBId != 348 ||
		created.ReleaseYear == nil || *created.ReleaseYear != 1979 ||
		created.PosterURL == nil || *created.PosterURL != "https://images.test/w500/alien.jpg" || len(created.MetadataJSON) == 0 {
		t.Fatalf("unexpected movie input: %+v", created)
	}
	if len(entries.created) != 1 || entries.created[0].GroupNumber != 4 {
		t.Fatalf("expected entry in group 4, got %+v", entries.created)
	}
	if !strings.Contains(recorder.Header().Get("HX-Trigger"), "Movie added!") {
		t.Fatalf("missing success trigger: %q", recorder.Header().Get("HX-Trigger"))
	}
}

func TestAddFromTMDBReusesExistingMovie(t *testing.T) {
	existing := &model.Movie{ID: uuid.New(), Title: "Alien"}
	movies := &stubMovieRepo{existing: existing}
	entries := &stubMovieEntryRepo{}
	client := &stubTMDBClient{}
	handler := &MovieHandler{movieRepo: movies, entryRepo: entries, tmdbClient: client}

	// A missing or invalid group falls back to group 1.
	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"nope"}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if client.getCalls != 0 || len(movies.created) != 0 {
		t.Fatalf("expected no TMDB fetch or movie create, got fetches=%d creates=%d", client.getCalls, len(movies.created))
	}
	if len(entries.created) != 1 || entries.created[0].MovieID != existing.ID || entries.created[0].GroupNumber != 1 {
		t.Fatalf("unexpected entry input: %+v", entries.created)
	}
}

func TestAddFromTMDBDuplicateEntryReturnsExisting(t *testing.T) {
	existing := &model.Movie{ID: uuid.New()}
	entries := &stubMovieEntryRepo{
		createErr: &pgconn.PgError{Code: "23505"},
		existing:  &model.Entry{ID: uuid.New(), MovieID: existing.ID, GroupNumber: 2},
	}
	handler := &MovieHandler{movieRepo: &stubMovieRepo{existing: existing}, entryRepo: entries, tmdbClient: &stubTMDBClient{}}

	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"2"}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if len(entries.lookupGroups) != 1 || entries.lookupGroups[0] != 2 {
		t.Fatalf("expected existing entry lookup in group 2, got %v", entries.lookupGroups)
	}
}

func TestAddFromTMDBErrors(t *testing.T) {
	cases := []struct {
		name    string
		form    url.Values
		client  *stubTMDBClient
		entries *stubMovieEntryRepo
		want    int
	}{
		{"invalid tmdb id", url.Values{"tmdb_id": {"abc"}}, &stubTMDBClient{}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"tmdb failure", url.Values{"tmdb_id": {"1"}}, &stubTMDBClient{err: errors.New("down")}, &stubMovieEntryRepo{}, http.StatusInternalServerError},
		{"tmdb not found", url.Values{"tmdb_id": {"1"}}, &stubTMDBClient{}, &stubMovieEntryRepo{}, http.StatusNotFound},
		{"entry create failure", url.Values{"tmdb_id": {"1"}}, &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}, &stubMovieEntryRepo{createErr: errors.New("db down")}, http.StatusInternalServerError},
		{"duplicate vanished", url.Values{"tmdb_id": {"1"}}, &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}, &stubMovieEntryRepo{createErr: &pgconn.PgError{Code: "23505"}}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &MovieHandler{movieRepo: &stubMovieRepo{}, entryRepo: tc.entries, tmdbClient: tc.client}
			recorder := postAddFromTMDB(handler, tc.form)
			if recorder.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, recorder.Code)
			}
			if recorder.Header().Get("HX-Trigger") != "" {
				t.Fatal("expected no success trigger")
			}
		})
	}
}
