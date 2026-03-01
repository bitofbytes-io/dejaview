package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sort"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

// DashboardHandler handles the main dashboard
type DashboardHandler struct {
	entryRepo  *repository.EntryRepository
	personRepo *repository.PersonRepository
	apiToken   string
}

// NewDashboardHandler creates a new DashboardHandler
func NewDashboardHandler(entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, apiToken string) *DashboardHandler {
	return &DashboardHandler{
		entryRepo:  entryRepo,
		personRepo: personRepo,
		apiToken:   apiToken,
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

	isAuthenticated := isAuthenticatedRequest(r, h.apiToken)
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

	isAuthenticated := isAuthenticatedRequest(r, h.apiToken)
	pages.DashboardContent(groupDataList, persons, currentGroup, isAuthenticated).Render(r.Context(), w)
}

// getDashboardData retrieves all data needed for the dashboard
func (h *DashboardHandler) getDashboardData(ctx context.Context) ([]pages.GroupData, []*model.Person, int, error) {
	// Get all group numbers
	groups, err := h.entryRepo.ListGroups(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	// Get persons for rating display
	persons, err := h.personRepo.GetAll(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	// Get current group for adding movies
	currentGroup, err := h.entryRepo.GetCurrentGroup(ctx)
	if err != nil {
		slog.Error("failed to get current group", "error", err)
		currentGroup = 1
	}

	// Build group data with entries
	groupDataList := make([]pages.GroupData, 0, len(groups))
	for _, groupNum := range groups {
		entries, err := h.entryRepo.ListByGroup(ctx, groupNum)
		if err != nil {
			slog.Error("failed to list entries for group", "group", groupNum, "error", err)
			continue
		}
		groupDataList = append(groupDataList, pages.GroupData{
			Number:  groupNum,
			Entries: entries,
		})
	}

	// Sort groups by group number (descending), so higher group numbers appear first
	sort.Slice(groupDataList, func(i, j int) bool {
		return groupDataList[i].Number > groupDataList[j].Number
	})

	return groupDataList, persons, currentGroup, nil
}


