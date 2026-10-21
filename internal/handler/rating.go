package handler

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/drywaters/dejaview/internal/metrics"
	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RatingHandler handles rating-related requests
type RatingHandler struct {
	metrics    *metrics.Metrics
	ratingRepo ratingRepository
	entryRepo  entryRepository
	personRepo personRepository
}

type ratingRepository interface {
	SaveBatch(ctx context.Context, entryID uuid.UUID, changes []model.RatingChange) error
}

type entryRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error)
}

type personRepository interface {
	GetAll(ctx context.Context) ([]*model.Person, error)
}

// NewRatingHandler creates a new RatingHandler
func NewRatingHandler(ratingRepo *repository.RatingRepository, entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, m *metrics.Metrics) *RatingHandler {
	return &RatingHandler{
		metrics:    m,
		ratingRepo: ratingRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}
}

// SaveRatings handles saving all ratings in one request
func (h *RatingHandler) SaveRatings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entryIDStr := chi.URLParam(r, "id")
	entryID, err := uuid.Parse(entryIDStr)
	if err != nil {
		http.Error(w, "Invalid entry ID", http.StatusBadRequest)
		return
	}

	if err := parseLimitedForm(w, r); err != nil {
		if requestBodyTooLarge(w, err) {
			return
		}
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Get the entry to have current state
	entry, err := h.entryRepo.GetByID(ctx, entryID)
	if err != nil {
		slog.Error("failed to get entry", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	changes, err := parseRatingChanges(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scoreCount := 0
	for _, change := range changes {
		if change.Score != nil {
			scoreCount++
		}
	}
	if len(changes) > 0 {
		if err := h.ratingRepo.SaveBatch(ctx, entryID, changes); err != nil {
			h.metrics.RatingsSaved("failure", scoreCount)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if errors.Is(err, repository.ErrPersonNotFound) {
				http.Error(w, "Unknown person ID", http.StatusBadRequest)
				return
			}
			if errors.Is(err, repository.ErrEntryNotFound) {
				http.Error(w, "Entry not found", http.StatusNotFound)
				return
			}
			slog.Error("failed to save ratings", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.metrics.RatingsSaved("success", scoreCount)
	}

	// Fetch updated entry and persons for response
	entry, err = h.entryRepo.GetByID(ctx, entryID)
	if err != nil {
		slog.Error("failed to get entry", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		slog.Error("entry not found after refetch", "entryID", entryID)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	persons, err := h.personRepo.GetAll(ctx)
	if err != nil {
		slog.Error("failed to get persons", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	slog.Info("ratings saved", "entry_id", entryID, "changes", len(changes))
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Saved!", "type": "success"}}`)
	if err := pages.RatingsCard(entry, persons, isAuthenticated).Render(ctx, w); err != nil {
		slog.Error("failed to render ratings card", "error", err)
	}
}

// parseRatingChanges validates the entire submission before any rating is written.
func parseRatingChanges(form url.Values) ([]model.RatingChange, error) {
	var changes []model.RatingChange
	seen := make(map[uuid.UUID]bool)
	for key, values := range form {
		if !strings.HasPrefix(key, "rating[") {
			continue
		}
		if !strings.HasSuffix(key, "]") || len(values) != 1 {
			return nil, errors.New("invalid or duplicate rating field")
		}
		personID, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(key, "rating["), "]"))
		if err != nil || personID == uuid.Nil {
			return nil, errors.New("invalid person ID")
		}
		if seen[personID] {
			return nil, errors.New("duplicate rating for person")
		}
		seen[personID] = true
		change := model.RatingChange{PersonID: personID}
		if value := strings.TrimSpace(values[0]); value != "" {
			score, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 10 {
				return nil, errors.New("ratings must be numbers from 0 to 10")
			}
			change.Score = &score
		}
		changes = append(changes, change)
	}
	// Stable locking order also avoids deadlocks between overlapping submissions.
	sort.Slice(changes, func(i, j int) bool { return changes[i].PersonID.String() < changes[j].PersonID.String() })
	return changes, nil
}
