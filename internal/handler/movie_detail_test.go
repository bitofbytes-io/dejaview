package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubDetailEntryRepo struct {
	stubMovieEntryRepo
	entry *model.Entry
	err   error
}

func (s *stubDetailEntryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.entry != nil && s.entry.ID == id {
		return s.entry, nil
	}
	return nil, nil
}

func getMovieDetail(h *MovieHandler, id string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Get("/movies/{id}", h.MovieDetailPage)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/"+id, nil))
	return rec
}

func TestMovieDetailPageRendersFromStoredMetadata(t *testing.T) {
	picker := &model.Person{ID: uuid.New(), Initial: "J", Name: "Jennifer", Color: "rose"}
	other := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel", Color: "cyan"}
	imdb := "tt0113277"
	synopsis := "A group of professional bank robbers start to feel the heat."
	entry := &model.Entry{
		ID:               uuid.New(),
		GroupNumber:      3,
		PickedByPersonID: &picker.ID,
		PickedByPerson:   picker,
		Movie: &model.Movie{
			Title:        "Heat",
			Synopsis:     &synopsis,
			IMDBId:       &imdb,
			MetadataJSON: []byte(`{"tagline":"A los angeles crime saga","backdrop_path":"/heat.jpg","genres":[{"id":80,"name":"Crime"}]}`),
		},
		Ratings: []*model.Rating{{PersonID: picker.ID, Score: 9, Person: picker}},
	}
	tmdbStub := &stubTMDBClient{}
	h := &MovieHandler{
		entryRepo:  &stubDetailEntryRepo{entry: entry},
		personRepo: &stubPersonRepo{persons: []*model.Person{picker, other}},
		tmdbClient: tmdbStub,
	}

	rec := getMovieDetail(h, entry.ID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Heat",
		"A los angeles crime saga",
		"https://image.tmdb.org/t/p/w1280/heat.jpg",
		">Crime</span>",
		"https://www.imdb.com/title/tt0113277/",
		synopsis,
		"Picked by Jennifer",
		"Daniel",
		">9.0</span>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q", want)
		}
	}
	if tmdbStub.getCalls != 0 {
		t.Errorf("rendering the page called TMDB %d times, want 0", tmdbStub.getCalls)
	}
}

func TestMovieDetailPageWithoutOptionalMetadata(t *testing.T) {
	entry := &model.Entry{ID: uuid.New(), Movie: &model.Movie{Title: "Alien"}}
	h := &MovieHandler{
		entryRepo:  &stubDetailEntryRepo{entry: entry},
		personRepo: &stubPersonRepo{},
		tmdbClient: &stubTMDBClient{},
	}

	rec := getMovieDetail(h, entry.ID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Alien") {
		t.Error("body is missing the title")
	}
	for _, unwanted := range []string{"detail-backdrop", "detail-tagline", "detail-tags", "imdb.com", "Overview"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body contains %q for a movie with no metadata", unwanted)
		}
	}
}

func TestMovieDetailPageErrors(t *testing.T) {
	tests := []struct {
		name string
		id   string
		repo *stubDetailEntryRepo
		want int
	}{
		{"unknown entry", uuid.NewString(), &stubDetailEntryRepo{}, http.StatusNotFound},
		{"invalid id", "not-a-uuid", &stubDetailEntryRepo{}, http.StatusBadRequest},
		{"repository failure", uuid.NewString(), &stubDetailEntryRepo{err: errors.New("db down")}, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MovieHandler{entryRepo: tt.repo, personRepo: &stubPersonRepo{}, tmdbClient: &stubTMDBClient{}}
			if rec := getMovieDetail(h, tt.id); rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
