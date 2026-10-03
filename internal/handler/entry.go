package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// EntryHandler handles entry-related requests
type EntryHandler struct {
	entryRepo  entryStore
	personRepo *repository.PersonRepository
}

type entryStore interface {
	Update(ctx context.Context, id uuid.UUID, input model.UpdateEntryInput) error
	Delete(ctx context.Context, id uuid.UUID) error
	ReorderEntries(ctx context.Context, groupNumber int, entryIDs []uuid.UUID) error
}

// NewEntryHandler creates a new EntryHandler
func NewEntryHandler(entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository) *EntryHandler {
	return &EntryHandler{
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}
}

// Update updates an entry
func (h *EntryHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	input := model.UpdateEntryInput{}

	if groupStr := r.FormValue("group_number"); groupStr != "" {
		groupNumber, err := strconv.Atoi(groupStr)
		if err != nil || groupNumber < 1 {
			http.Error(w, "Invalid group_number", http.StatusBadRequest)
			return
		}
		input.GroupNumber = &groupNumber
	}

	if _, ok := r.Form["picked_by_person_id"]; ok {
		pickedByStr := r.FormValue("picked_by_person_id")
		if pickedByStr == "" {
			nilID := uuid.Nil
			input.PickedByPersonID = &nilID
		} else {
			pickedByID, err := uuid.Parse(pickedByStr)
			if err != nil {
				slog.Warn("invalid picked_by_person_id", "error", err, "picked_by_person_id", pickedByStr, "entry_id", entryID)
				http.Error(w, "Invalid picked_by_person_id", http.StatusBadRequest)
				return
			}
			input.PickedByPersonID = &pickedByID
		}
	}

	err = h.entryRepo.Update(ctx, entryID, input)
	if errors.Is(err, repository.ErrEntryNotFound) {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, repository.ErrEntryExistsInGroup) {
		http.Error(w, "Movie already exists in that group", http.StatusConflict)
		return
	}
	if errors.Is(err, repository.ErrEntryGroupChanged) {
		http.Error(w, "Entry was moved by another request; refresh and try again", http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("failed to update entry", "error", err)
		http.Error(w, "Failed to update entry", http.StatusInternalServerError)
		return
	}

	slog.Info("entry updated", "entry_id", entryID)
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Entry updated!", "type": "success"}, "refreshGroups": true}`)
	w.WriteHeader(http.StatusOK)
}

// Delete removes an entry
func (h *EntryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entryIDStr := chi.URLParam(r, "id")
	entryID, err := uuid.Parse(entryIDStr)
	if err != nil {
		http.Error(w, "Invalid entry ID", http.StatusBadRequest)
		return
	}

	if err := h.entryRepo.Delete(ctx, entryID); err != nil {
		slog.Error("failed to delete entry", "error", err)
		http.Error(w, "Failed to delete entry", http.StatusInternalServerError)
		return
	}

	slog.Info("entry deleted", "entry_id", entryID)
	w.Header().Set("HX-Trigger", `{"showToast": {"message": "Entry deleted!", "type": "success"}, "refreshGroups": true}`)
	w.WriteHeader(http.StatusOK)
}

// ReorderRequest represents the JSON body for reordering entries
type ReorderRequest struct {
	EntryIDs []string `json:"entry_ids"`
}

// Reorder updates the order of entries within a group
func (h *EntryHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	groupNumStr := chi.URLParam(r, "num")
	groupNum, err := strconv.Atoi(groupNumStr)
	if err != nil {
		http.Error(w, "Invalid group number", http.StatusBadRequest)
		return
	}

	var req ReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Convert string IDs to UUIDs
	entryIDs := make([]uuid.UUID, 0, len(req.EntryIDs))
	for _, idStr := range req.EntryIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			slog.Warn("invalid entry id in reorder request", "entry_id", idStr, "error", err)
			http.Error(w, "Invalid entry ID", http.StatusBadRequest)
			return
		}
		entryIDs = append(entryIDs, id)
	}

	err = h.entryRepo.ReorderEntries(ctx, groupNum, entryIDs)
	if errors.Is(err, repository.ErrReorderMismatch) {
		slog.Info("reorder rejected", "group_number", groupNum, "error", err)
		http.Error(w, "The group changed; refresh and try again", http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("failed to reorder entries", "error", err)
		http.Error(w, "Failed to reorder entries", http.StatusInternalServerError)
		return
	}

	slog.Info("entries reordered", "group_number", groupNum, "entry_count", len(entryIDs))
	w.WriteHeader(http.StatusOK)
}
