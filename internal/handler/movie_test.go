package handler

import (
	"context"
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

func TestSearchTMDBRejectsOverlongQuery(t *testing.T) {
	handler := NewMovieHandler(nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/tmdb/search?q="+strings.Repeat("a", 121), nil)
	recorder := httptest.NewRecorder()

	handler.SearchTMDB(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

type stubMovieRepo struct {
	existing    *model.Movie
	createErr   error
	created     []model.CreateMovieInput
	getByTMDBID []int
}

func (s *stubMovieRepo) GetByTMDBId(ctx context.Context, tmdbID int) (*model.Movie, error) {
	s.getByTMDBID = append(s.getByTMDBID, tmdbID)
	return s.existing, nil
}

func (s *stubMovieRepo) Create(ctx context.Context, input model.CreateMovieInput) (*model.Movie, error) {
	s.created = append(s.created, input)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &model.Movie{ID: uuid.New(), Title: input.Title, TMDBId: input.TMDBId}, nil
}

type stubMovieEntryRepo struct {
	beforeCreate func()
	checkErr     error
	createErr    error
	created      []model.CreateEntryInput
}

func (s *stubMovieEntryRepo) CheckGroup(ctx context.Context, group int) error {
	if s.checkErr != nil {
		return s.checkErr
	}
	if group < 1 {
		return repository.ErrInvalidGroup
	}
	return nil
}

func (s *stubMovieEntryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	return nil, nil
}

func (s *stubMovieEntryRepo) Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error) {
	if s.beforeCreate != nil {
		s.beforeCreate()
	}
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
		created.PosterURL == nil || *created.PosterURL != "https://images.test/w500/alien.jpg" || len(created.MetadataJSON) == 0 ||
		created.RuntimeMinutes == nil || *created.RuntimeMinutes != 117 {
		t.Fatalf("unexpected movie input: %+v", created)
	}
	if len(entries.created) != 1 || entries.created[0].GroupNumber != 4 {
		t.Fatalf("expected entry in group 4, got %+v", entries.created)
	}
	if !strings.Contains(recorder.Header().Get("HX-Trigger"), "Movie added!") {
		t.Fatalf("missing success trigger: %q", recorder.Header().Get("HX-Trigger"))
	}
}

func TestAddFromTMDBStoresZeroRuntimeAsUnknown(t *testing.T) {
	movies := &stubMovieRepo{}
	client := &stubTMDBClient{details: &tmdb.MovieDetails{ID: 99, Title: "Untimed", Runtime: 0}}
	handler := &MovieHandler{movieRepo: movies, entryRepo: &stubMovieEntryRepo{}, tmdbClient: client}

	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"99"}, "group_number": {"1"}})

	if recorder.Code != http.StatusOK || len(movies.created) != 1 {
		t.Fatalf("expected one created movie, got status %d and %d creates", recorder.Code, len(movies.created))
	}
	if runtime := movies.created[0].RuntimeMinutes; runtime != nil {
		t.Fatalf("expected unknown runtime, got %d", *runtime)
	}
}

func TestAddFromTMDBReusesExistingMovie(t *testing.T) {
	existing := &model.Movie{ID: uuid.New(), Title: "Alien"}
	movies := &stubMovieRepo{existing: existing}
	entries := &stubMovieEntryRepo{}
	client := &stubTMDBClient{}
	handler := &MovieHandler{movieRepo: movies, entryRepo: entries, tmdbClient: client}

	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"2"}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if client.getCalls != 0 || len(movies.created) != 0 {
		t.Fatalf("expected no TMDB fetch or movie create, got fetches=%d creates=%d", client.getCalls, len(movies.created))
	}
	if len(entries.created) != 1 || entries.created[0].MovieID != existing.ID || entries.created[0].GroupNumber != 2 {
		t.Fatalf("unexpected entry input: %+v", entries.created)
	}
}

func TestAddFromTMDBDuplicateEntryShowsAlreadyInGroup(t *testing.T) {
	existing := &model.Movie{ID: uuid.New()}
	entries := &stubMovieEntryRepo{createErr: repository.ErrEntryExistsInGroup}
	handler := &MovieHandler{movieRepo: &stubMovieRepo{existing: existing}, entryRepo: entries, tmdbClient: &stubTMDBClient{}}

	recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"2"}})

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, recorder.Code)
	}
	trigger := recorder.Header().Get("HX-Trigger")
	if !strings.Contains(trigger, "Already in Group 2") || strings.Contains(trigger, "Movie added!") || strings.Contains(trigger, "refreshGroups") {
		t.Fatalf("unexpected trigger: %q", trigger)
	}
}

func TestAddFromTMDBRejectsInvalidGroup(t *testing.T) {
	for _, group := range []string{"", "nope", "1.5", "0", "-1"} {
		t.Run(group, func(t *testing.T) {
			movies := &stubMovieRepo{}
			entries := &stubMovieEntryRepo{}
			client := &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}
			handler := &MovieHandler{movieRepo: movies, entryRepo: entries, tmdbClient: client}
			recorder := postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {group}})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}
			if client.getCalls != 0 || len(movies.created) != 0 || len(entries.created) != 0 {
				t.Fatalf("invalid group reached TMDB or the database: fetches=%d movies=%d entries=%d", client.getCalls, len(movies.created), len(entries.created))
			}
			if !strings.Contains(recorder.Header().Get("HX-Trigger"), "refreshGroups") {
				t.Fatalf("expected a refresh, got trigger %q", recorder.Header().Get("HX-Trigger"))
			}
		})
	}

	// A stale group past the next new one stores nothing, not even a new movie.
	movies := &stubMovieRepo{}
	client := &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}
	stale := &stubMovieEntryRepo{checkErr: repository.ErrInvalidGroup}
	recorder := postAddFromTMDB(&MovieHandler{movieRepo: movies, entryRepo: stale, tmdbClient: client}, url.Values{"tmdb_id": {"348"}, "group_number": {"99"}})
	if recorder.Code != http.StatusBadRequest || client.getCalls != 0 || len(movies.created) != 0 || len(stale.created) != 0 {
		t.Fatalf("stale group 99: status %d, fetches=%d movies=%d entries=%d; want 400 and no writes",
			recorder.Code, client.getCalls, len(movies.created), len(stale.created))
	}

	// A group that became invalid after the check is still rejected by Create.
	entries := &stubMovieEntryRepo{createErr: repository.ErrInvalidGroup}
	handler := &MovieHandler{movieRepo: &stubMovieRepo{existing: &model.Movie{ID: uuid.New()}}, entryRepo: entries, tmdbClient: &stubTMDBClient{}}
	recorder = postAddFromTMDB(handler, url.Values{"tmdb_id": {"348"}, "group_number": {"99"}})
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Header().Get("HX-Trigger"), "Movie added!") {
		t.Fatalf("group 99: status %d trigger %q, want 400 without success", recorder.Code, recorder.Header().Get("HX-Trigger"))
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
		{"invalid tmdb id", url.Values{"tmdb_id": {"abc"}, "group_number": {"1"}}, &stubTMDBClient{}, &stubMovieEntryRepo{}, http.StatusBadRequest},
		{"tmdb failure", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubTMDBClient{err: errors.New("down")}, &stubMovieEntryRepo{}, http.StatusInternalServerError},
		{"tmdb not found", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubTMDBClient{}, &stubMovieEntryRepo{}, http.StatusNotFound},
		{"group check failure", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}, &stubMovieEntryRepo{checkErr: errors.New("db down")}, http.StatusInternalServerError},
		{"entry create failure", url.Values{"tmdb_id": {"1"}, "group_number": {"1"}}, &stubTMDBClient{details: &tmdb.MovieDetails{Title: "X"}}, &stubMovieEntryRepo{createErr: errors.New("db down")}, http.StatusInternalServerError},
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

func TestEntryCreationMetrics(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		cached                       bool
		checkErr, movieErr, entryErr error
		tmdbErr                      error
		wantStatus, wantCalls        int
		want                         map[string]float64
	}{
		{name: "new movie", wantStatus: 200, wantCalls: 1, want: map[string]float64{`{result="success"}`: 1}},
		{name: "cached movie", cached: true, wantStatus: 200, want: map[string]float64{`{result="success"}`: 1}},
		{name: "duplicate", cached: true, entryErr: repository.ErrEntryExistsInGroup, wantStatus: 409, want: map[string]float64{`{result="failure"}`: 1}},
		{name: "entry database failure after movie insert", entryErr: errors.New("db failure"), wantCalls: 1, wantStatus: 500, want: map[string]float64{`{result="failure"}`: 1}},
		{name: "group became invalid", cached: true, entryErr: repository.ErrInvalidGroup, wantStatus: 400, want: map[string]float64{`{result="failure"}`: 1}},
		{name: "movie insert failure", movieErr: errors.New("movie db failure"), wantStatus: 500, wantCalls: 1, want: map[string]float64{}},
		{name: "tmdb failure", tmdbErr: errors.New("tmdb failure"), wantStatus: 500, wantCalls: 1, want: map[string]float64{}},
		{name: "invalid group before create", checkErr: repository.ErrInvalidGroup, wantStatus: 400, want: map[string]float64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := metrics.New()
			movies := &stubMovieRepo{createErr: tc.movieErr}
			if tc.cached {
				movies.existing = &model.Movie{ID: uuid.New()}
			}
			entries := &stubMovieEntryRepo{checkErr: tc.checkErr, createErr: tc.entryErr, beforeCreate: func() { assertDomainMetrics(t, m, "dejaview_entries_added_total", map[string]float64{}) }}
			client := &stubTMDBClient{details: &tmdb.MovieDetails{ID: 348, Title: "Private title"}, err: tc.tmdbErr}
			h := &MovieHandler{movieRepo: movies, entryRepo: entries, tmdbClient: client, metrics: m}
			assertDomainMetrics(t, m, "dejaview_entries_added_total", map[string]float64{})
			rec := postAddFromTMDB(h, url.Values{"tmdb_id": {"348"}, "group_number": {"1"}})
			if rec.Code != tc.wantStatus || client.getCalls != tc.wantCalls {
				t.Fatalf("status=%d calls=%d", rec.Code, client.getCalls)
			}
			assertDomainMetrics(t, m, "dejaview_entries_added_total", tc.want)
			// Only the real TMDB client counts its own HTTP requests, never the handler.
			assertDomainMetrics(t, m, "dejaview_tmdb_lookups_total", map[string]float64{})
		})
	}
}
