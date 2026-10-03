package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/drywaters/dejaview/internal/ui/partials"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// MovieHandler handles movie-related requests
type MovieHandler struct {
	movieRepo  movieRepository
	entryRepo  movieEntryRepository
	personRepo personRepository
	tmdbClient tmdbClient
}

type movieRepository interface {
	GetByTMDBId(ctx context.Context, tmdbID int) (*model.Movie, error)
	Create(ctx context.Context, input model.CreateMovieInput) (*model.Movie, error)
}

type movieEntryRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error)
	CheckGroup(ctx context.Context, group int) error
	Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error)
}

type tmdbClient interface {
	Search(ctx context.Context, query string) (*tmdb.SearchResponse, error)
	GetMovie(ctx context.Context, tmdbID int) (*tmdb.MovieDetails, error)
	PosterURL(path string, size string) string
}

// NewMovieHandler creates a new MovieHandler
func NewMovieHandler(movieRepo *repository.MovieRepository, entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, tmdbClient *tmdb.Client) *MovieHandler {
	return &MovieHandler{
		movieRepo:  movieRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
		tmdbClient: tmdbClient,
	}
}

// MovieDetailPage renders the movie detail page
func (h *MovieHandler) MovieDetailPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entryIDStr := chi.URLParam(r, "id")
	entryID, err := uuid.Parse(entryIDStr)
	if err != nil {
		http.Error(w, "Invalid entry ID", http.StatusBadRequest)
		return
	}

	entry, err := h.entryRepo.GetByID(ctx, entryID)
	if err != nil {
		slog.Error("failed to get entry", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}

	persons, err := h.personRepo.GetAll(ctx)
	if err != nil {
		slog.Error("failed to get persons", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	pages.MovieDetailPage(entry, persons, isAuthenticated).Render(ctx, w)
}

// SearchTMDB handles TMDB movie search
func (h *MovieHandler) SearchTMDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	if query == "" {
		partials.SearchResults(nil).Render(ctx, w)
		return
	}
	if len(query) > 120 {
		http.Error(w, "Search query too long", http.StatusBadRequest)
		return
	}

	results, err := h.tmdbClient.Search(ctx, query)
	if err != nil {
		slog.Error("TMDB search failed", "error", err)
		http.Error(w, "Search failed", http.StatusInternalServerError)
		return
	}

	partials.SearchResults(results.Results).Render(ctx, w)
}

// AddFromTMDB adds a movie from TMDB to the library
func (h *MovieHandler) AddFromTMDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	tmdbIDStr := r.FormValue("tmdb_id")
	groupNumberStr := r.FormValue("group_number")

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		http.Error(w, "Invalid TMDB ID", http.StatusBadRequest)
		return
	}

	// Reject a stale group before storing a new movie for it; Create checks
	// again under the group's lock.
	groupNumber, err := strconv.Atoi(groupNumberStr)
	if err != nil {
		rejectAddGroup(w)
		return
	}
	if err := h.entryRepo.CheckGroup(ctx, groupNumber); errors.Is(err, repository.ErrInvalidGroup) {
		rejectAddGroup(w)
		return
	} else if err != nil {
		slog.Error("failed to check group", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if movie already exists in library
	existingMovie, err := h.movieRepo.GetByTMDBId(ctx, tmdbID)
	if err != nil {
		slog.Error("failed to check existing movie", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var movie *model.Movie
	reusedMovie := existingMovie != nil
	if existingMovie != nil {
		movie = existingMovie
	} else {
		// Fetch movie details from TMDB
		details, err := h.tmdbClient.GetMovie(ctx, tmdbID)
		if err != nil {
			slog.Error("failed to get TMDB movie", "error", err)
			http.Error(w, "Failed to fetch movie details", http.StatusInternalServerError)
			return
		}
		if details == nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}

		// Build poster URL
		var posterURL *string
		if details.PosterPath != nil {
			url := h.tmdbClient.PosterURL(*details.PosterPath, "w500")
			posterURL = &url
		}

		// Store metadata as JSON
		metadataJSON, err := json.Marshal(details)
		if err != nil {
			slog.Error("failed to marshal TMDB metadata", "error", err, "tmdb_id", tmdbID)
			http.Error(w, "Failed to save movie metadata", http.StatusInternalServerError)
			return
		}

		// Create movie in database
		movie, err = h.movieRepo.Create(ctx, model.CreateMovieInput{
			Title:          details.Title,
			ReleaseYear:    tmdb.ReleaseYear(details.ReleaseDate),
			PosterURL:      posterURL,
			Synopsis:       &details.Overview,
			RuntimeMinutes: &details.Runtime,
			TMDBId:         &tmdbID,
			IMDBId:         details.IMDBId,
			MetadataJSON:   metadataJSON,
		})
		if err != nil {
			slog.Error("failed to create movie", "error", err)
			http.Error(w, "Failed to save movie", http.StatusInternalServerError)
			return
		}
	}

	// Create entry for this movie
	entry, err := h.entryRepo.Create(ctx, model.CreateEntryInput{
		MovieID:     movie.ID,
		GroupNumber: groupNumber,
	})
	if errors.Is(err, repository.ErrInvalidGroup) {
		rejectAddGroup(w)
		return
	}
	if errors.Is(err, repository.ErrEntryExistsInGroup) {
		slog.Info("movie already in group", "movie_id", movie.ID, "tmdb_id", tmdbID, "group_number", groupNumber)
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"showToast": {"message": "Already in Group %d", "type": "error"}}`, groupNumber))
		http.Error(w, "Movie is already in that group", http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("failed to create entry", "error", err)
		http.Error(w, "Failed to create entry", http.StatusInternalServerError)
		return
	}

	// Return success with HX-Trigger to refresh the group
	slog.Info("movie entry added", "entry_id", entry.ID, "movie_id", movie.ID, "tmdb_id", tmdbID, "group_number", groupNumber, "reused_movie", reusedMovie)
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Movie added!", "type": "success"}, "refreshGroups": true}`)
	w.WriteHeader(http.StatusOK)
}

// rejectAddGroup answers an add whose group is not an existing group or the
// next new one, which a stale dashboard can send, and refreshes the groups.
func rejectAddGroup(w http.ResponseWriter) {
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Pick a group from the list", "type": "error"}, "refreshGroups": true}`)
	http.Error(w, "Invalid group number", http.StatusBadRequest)
}
