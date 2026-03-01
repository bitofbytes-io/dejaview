package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/partials"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RatingHandler handles rating-related requests
type RatingHandler struct {
	ratingRepo ratingRepository
	entryRepo  entryRepository
	personRepo personRepository
}

type ratingRepository interface {
	Upsert(ctx context.Context, input model.UpsertRatingInput) (*model.Rating, error)
	Delete(ctx context.Context, personID, entryID uuid.UUID) error
}

type entryRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error)
}

type personRepository interface {
	GetAll(ctx context.Context) ([]*model.Person, error)
}

// NewRatingHandler creates a new RatingHandler
func NewRatingHandler(ratingRepo *repository.RatingRepository, entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository) *RatingHandler {
	return &RatingHandler{
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

	if err := r.ParseForm(); err != nil {
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

	// Build a map of existing ratings for quick lookup
	existingRatings := make(map[uuid.UUID]bool)
	for _, r := range entry.Ratings {
		existingRatings[r.PersonID] = true
	}

	// Process ratings from form: rating[personID] = score
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "rating[") || !strings.HasSuffix(key, "]") {
			continue
		}

		// Extract person ID from rating[uuid]
		personIDStr := strings.TrimSuffix(strings.TrimPrefix(key, "rating["), "]")
		personID, err := uuid.Parse(personIDStr)
		if err != nil {
			slog.Warn("invalid person ID in rating form", "key", key, "error", err)
			continue
		}

		scoreStr := ""
		if len(values) > 0 {
			scoreStr = strings.TrimSpace(values[0])
		}

		if scoreStr == "" {
			// Empty score - delete the rating if it exists
			if existingRatings[personID] {
				if err := h.ratingRepo.Delete(ctx, personID, entryID); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					slog.Error("failed to delete rating", "error", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
			}
		} else {
			// Parse and save the rating
			score, err := strconv.ParseFloat(scoreStr, 64)
			if err != nil || score < 0.0 || score > 10.0 {
				slog.Warn("invalid score value", "score", scoreStr, "personID", personID)
				continue
			}

			_, err = h.ratingRepo.Upsert(ctx, model.UpsertRatingInput{
				PersonID: personID,
				EntryID:  entryID,
				Score:    score,
			})
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				slog.Error("failed to save rating", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}
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

	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Saved!", "type": "success"}}`)
	partials.RatingsUpdate(entry, persons, true).Render(ctx, w)
}
