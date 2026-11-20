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

	"github.com/drywaters/dejaview/internal/metrics"
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
	metrics    *metrics.Metrics
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
	GroupState(ctx context.Context) (model.EntryGroupState, error)
	Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error)
}

type tmdbClient interface {
	Search(ctx context.Context, query string) (*tmdb.SearchResponse, error)
	GetMovie(ctx context.Context, tmdbID int) (*tmdb.MovieDetails, error)
	PosterURL(path string, size string) string
}

// NewMovieHandler creates a new MovieHandler
func NewMovieHandler(movieRepo *repository.MovieRepository, entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, tmdbClient *tmdb.Client, m *metrics.Metrics) *MovieHandler {
	return &MovieHandler{
		metrics:    m,
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
	if err := pages.MovieDetailPage(entry, persons, isAuthenticated).Render(ctx, w); err != nil {
		slog.Error("failed to render movie detail page", "error", err)
	}
}

// SearchTMDB handles TMDB movie search
func (h *MovieHandler) SearchTMDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	if query == "" {
		if err := partials.SearchResults(nil).Render(ctx, w); err != nil {
			slog.Error("failed to render empty search results", "error", err)
		}
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

	if err := partials.SearchResults(results.Results).Render(ctx, w); err != nil {
		slog.Error("failed to render search results", "error", err)
	}
}

// AddFromTMDB adds a movie from TMDB to the library
func (h *MovieHandler) AddFromTMDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	req, err := h.parseAddRequest(w, r)
	if err != nil {
		writeAddError(w, err)
		return
	}
	movie, reusedMovie, err := h.resolveMovie(ctx, req.tmdbID)
	if err != nil {
		writeAddError(w, err)
		return
	}
	entry, err := h.insertEntry(ctx, movie, req)
	if err != nil {
		writeAddError(w, err)
		return
	}

	// Return success with HX-Trigger to refresh the group
	slog.Info("movie entry added", "entry_id", entry.ID, "movie_id", movie.ID, "tmdb_id", req.tmdbID, "group_number", req.groupNumber, "reused_movie", reusedMovie)
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Movie added!", "type": "success"}, "refreshGroups": true}`)
	w.WriteHeader(http.StatusOK)
}

// addRequest is a validated add-from-TMDB form.
type addRequest struct {
	tmdbID       int
	groupNumber  int
	groupVersion int64
}

// addError is a failed add step with the response it maps to. Group
// rejections use the errAddInvalidGroup and errAddGroupChanged sentinels
// because their responses are shared with other handlers.
type addError struct {
	status  int
	message string
	trigger string
}

func (e *addError) Error() string { return e.message }

var (
	errAddInvalidGroup = errors.New("invalid group number")
	errAddGroupChanged = errors.New("groups changed")
)

func addInternalError(message string) *addError {
	return &addError{status: http.StatusInternalServerError, message: message}
}

// writeAddError writes the response for an error returned by an add step.
func writeAddError(w http.ResponseWriter, err error) {
	var addErr *addError
	switch {
	case errors.Is(err, errAddInvalidGroup):
		rejectAddGroup(w)
	case errors.Is(err, errAddGroupChanged):
		rejectChangedGroup(w)
	case errors.As(err, &addErr):
		if addErr.trigger != "" {
			w.Header().Set("HX-Trigger", addErr.trigger)
		}
		http.Error(w, addErr.message, addErr.status)
	default:
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// parseAddRequest parses the add form and validates its group and group
// version against current state before anything is stored.
func (h *MovieHandler) parseAddRequest(w http.ResponseWriter, r *http.Request) (addRequest, error) {
	ctx := r.Context()

	if err := parseLimitedForm(w, r); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			return addRequest{}, &addError{status: http.StatusRequestEntityTooLarge, message: "Request body too large"}
		}
		return addRequest{}, &addError{status: http.StatusBadRequest, message: "Invalid form data"}
	}

	tmdbID, err := strconv.Atoi(r.FormValue("tmdb_id"))
	if err != nil {
		return addRequest{}, &addError{status: http.StatusBadRequest, message: "Invalid TMDB ID"}
	}

	// Reject a stale group before storing a new movie for it; Create checks
	// again under the group's lock.
	groupNumber, err := strconv.Atoi(r.FormValue("group_number"))
	if err != nil {
		return addRequest{}, errAddInvalidGroup
	}
	state, err := h.entryRepo.GroupState(ctx)
	if err != nil {
		slog.Error("failed to read group state", "error", err)
		return addRequest{}, addInternalError("Internal Server Error")
	}
	version := state.Version
	if supplied := r.FormValue("group_version"); supplied != "" {
		parsed, err := strconv.ParseInt(supplied, 10, 64)
		if err != nil || parsed < 0 {
			return addRequest{}, &addError{status: http.StatusBadRequest, message: "Invalid group_version"}
		}
		if parsed != version {
			return addRequest{}, errAddGroupChanged
		}
		version = parsed
	}
	if err := h.entryRepo.CheckGroup(ctx, groupNumber); errors.Is(err, repository.ErrInvalidGroup) {
		return addRequest{}, errAddInvalidGroup
	} else if err != nil {
		slog.Error("failed to check group", "error", err)
		return addRequest{}, addInternalError("Internal Server Error")
	}

	return addRequest{tmdbID: tmdbID, groupNumber: groupNumber, groupVersion: version}, nil
}

// resolveMovie returns the library movie for a TMDB ID, fetching it from TMDB
// and storing it when it is not in the library yet. reused reports whether the
// movie already existed.
func (h *MovieHandler) resolveMovie(ctx context.Context, tmdbID int) (movie *model.Movie, reused bool, err error) {
	existing, err := h.movieRepo.GetByTMDBId(ctx, tmdbID)
	if err != nil {
		slog.Error("failed to check existing movie", "error", err)
		return nil, false, addInternalError("Internal Server Error")
	}
	if existing != nil {
		return existing, true, nil
	}

	details, err := h.tmdbClient.GetMovie(ctx, tmdbID)
	if err != nil {
		slog.Error("failed to get TMDB movie", "error", err)
		return nil, false, addInternalError("Failed to fetch movie details")
	}
	if details == nil {
		return nil, false, &addError{status: http.StatusNotFound, message: "Movie not found"}
	}

	var posterURL *string
	if details.PosterPath != nil {
		url := h.tmdbClient.PosterURL(*details.PosterPath, "w500")
		posterURL = &url
	}

	// Store metadata as JSON
	metadataJSON, err := json.Marshal(details)
	if err != nil {
		slog.Error("failed to marshal TMDB metadata", "error", err, "tmdb_id", tmdbID)
		return nil, false, addInternalError("Failed to save movie metadata")
	}

	movie, err = h.movieRepo.Create(ctx, model.CreateMovieInput{
		Title:          details.Title,
		ReleaseYear:    tmdb.ReleaseYear(details.ReleaseDate),
		PosterURL:      posterURL,
		Synopsis:       &details.Overview,
		RuntimeMinutes: tmdb.RuntimeMinutes(details.Runtime),
		TMDBId:         &tmdbID,
		IMDBId:         details.IMDBId,
		MetadataJSON:   metadataJSON,
	})
	if err != nil {
		slog.Error("failed to create movie", "error", err)
		return nil, false, addInternalError("Failed to save movie")
	}
	return movie, false, nil
}

// insertEntry creates the entry for the movie in the requested group and
// records the dejaview_entries_added_total outcome of the attempt.
func (h *MovieHandler) insertEntry(ctx context.Context, movie *model.Movie, req addRequest) (*model.Entry, error) {
	entry, err := h.entryRepo.Create(ctx, model.CreateEntryInput{
		MovieID:      movie.ID,
		GroupNumber:  req.groupNumber,
		GroupVersion: &req.groupVersion,
	})
	switch {
	case errors.Is(err, repository.ErrCommitOutcomeUnknown):
		h.metrics.EntryAdded("unknown")
	case err != nil:
		h.metrics.EntryAdded("failure")
	default:
		h.metrics.EntryAdded("success")
	}

	switch {
	case errors.Is(err, repository.ErrGroupNumberingChanged):
		return nil, errAddGroupChanged
	case errors.Is(err, repository.ErrInvalidGroup):
		return nil, errAddInvalidGroup
	case errors.Is(err, repository.ErrEntryExistsInGroup):
		slog.Info("movie already in group", "movie_id", movie.ID, "tmdb_id", req.tmdbID, "group_number", req.groupNumber)
		return nil, &addError{
			status:  http.StatusConflict,
			message: "Movie is already in that group",
			trigger: fmt.Sprintf(`{"showToast": {"message": "Already in Group %d", "type": "error"}}`, req.groupNumber),
		}
	case err != nil:
		slog.Error("failed to create entry", "error", err)
		return nil, addInternalError("Failed to create entry")
	}
	return entry, nil
}

// rejectAddGroup answers an add whose group is not an existing group or the
// next new one, which a stale dashboard can send, and refreshes the groups.
func rejectAddGroup(w http.ResponseWriter) {
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Pick a group from the list", "type": "error"}, "refreshGroups": true}`)
	http.Error(w, "Invalid group number", http.StatusBadRequest)
}
