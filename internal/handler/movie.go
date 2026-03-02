package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/drywaters/dejaview/internal/ui/partials"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// MovieHandler handles movie-related requests
type MovieHandler struct {
	movieRepo  *repository.MovieRepository
	entryRepo  *repository.EntryRepository
	personRepo *repository.PersonRepository
	tmdbClient *tmdb.Client
	apiToken   string
}

// NewMovieHandler creates a new MovieHandler
func NewMovieHandler(movieRepo *repository.MovieRepository, entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, tmdbClient *tmdb.Client, apiToken string) *MovieHandler {
	return &MovieHandler{
		movieRepo:  movieRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
		tmdbClient: tmdbClient,
		apiToken:   apiToken,
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

	isAuthenticated := isAuthenticatedRequest(r, h.apiToken)
	pages.MovieDetailPage(entry, persons, isAuthenticated).Render(ctx, w)
}

// SearchTMDB handles TMDB movie search
func (h *MovieHandler) SearchTMDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query().Get("q")

	if query == "" {
		partials.SearchResults(nil).Render(ctx, w)
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

	groupNumber, err := strconv.Atoi(groupNumberStr)
	if err != nil {
		groupNumber = 1
	}

	// Check if movie already exists in library
	existingMovie, err := h.movieRepo.GetByTMDBId(ctx, tmdbID)
	if err != nil {
		slog.Error("failed to check existing movie", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var movie *model.Movie
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
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			entry, err = h.entryRepo.GetByMovieAndGroup(ctx, movie.ID, groupNumber)
			if err != nil {
				slog.Error("failed to get existing entry", "error", err)
				http.Error(w, "Failed to retrieve entry", http.StatusInternalServerError)
				return
			}
			if entry == nil {
				slog.Error("duplicate entry reported but not found", "error", err)
				http.Error(w, "Failed to create entry", http.StatusInternalServerError)
				return
			}
		} else {
			slog.Error("failed to create entry", "error", err)
			http.Error(w, "Failed to create entry", http.StatusInternalServerError)
			return
		}
	}

	// Return success with HX-Trigger to refresh the group
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Movie added!", "type": "success"}, "refreshGroups": true}`)
	w.WriteHeader(http.StatusOK)
}

