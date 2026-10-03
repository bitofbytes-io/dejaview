package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

// DashboardHandler handles the main dashboard
type DashboardHandler struct {
	entryRepo  dashboardEntryRepository
	personRepo personRepository
}

type dashboardEntryRepository interface {
	ListAll(ctx context.Context) ([]*model.Entry, error)
}

// NewDashboardHandler creates a new DashboardHandler
func NewDashboardHandler(entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository) *DashboardHandler {
	return &DashboardHandler{
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}
}

// DashboardPage renders the main dashboard with all groups
func (h *DashboardHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	groupDataList, persons, currentGroup, err := h.getDashboardData(r.Context())
	if err != nil {
		slog.Error("failed to get dashboard data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	pages.DashboardPage(groupDataList, persons, currentGroup, isAuthenticated).Render(r.Context(), w)
}

// DashboardContent renders just the inner content for HTMX partial updates
func (h *DashboardHandler) DashboardContent(w http.ResponseWriter, r *http.Request) {
	groupDataList, persons, currentGroup, err := h.getDashboardData(r.Context())
	if err != nil {
		slog.Error("failed to get dashboard data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	pages.DashboardContent(groupDataList, persons, currentGroup, isAuthenticated).Render(r.Context(), w)
}

// getDashboardData loads every entry in one query and groups it for the
// dashboard. The current group, where new movies go by default, is the
// highest group number, or 1 when there are no entries.
func (h *DashboardHandler) getDashboardData(ctx context.Context) ([]pages.GroupData, []*model.Person, int, error) {
	entries, err := h.entryRepo.ListAll(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	// Get persons for rating display
	persons, err := h.personRepo.GetAll(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	groups := groupEntries(entries)
	currentGroup := 1
	if len(groups) > 0 {
		currentGroup = groups[0].Number
	}
	return groups, persons, currentGroup, nil
}

// groupEntries splits entries, which ListAll returns ordered by group
// number descending, into one GroupData per group in that order.
func groupEntries(entries []*model.Entry) []pages.GroupData {
	var groups []pages.GroupData
	for _, entry := range entries {
		if len(groups) == 0 || groups[len(groups)-1].Number != entry.GroupNumber {
			groups = append(groups, pages.GroupData{Number: entry.GroupNumber})
		}
		last := &groups[len(groups)-1]
		last.Entries = append(last.Entries, entry)
	}
	return groups
}
