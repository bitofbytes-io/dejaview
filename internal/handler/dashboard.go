package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

// meCookie remembers whose unrated movies the dashboard shows, so the
// needs-rating filter sticks across visits and HTMX refreshes. It is not
// authentication, only a default for the filter.
const (
	meCookie       = "dv_me"
	meCookieMaxAge = 365 * 24 * 60 * 60
)

// DashboardHandler handles the main dashboard
type DashboardHandler struct {
	entryRepo     dashboardEntryRepository
	personRepo    personRepository
	secureCookies bool
}

type dashboardEntryRepository interface {
	ListAll(ctx context.Context) ([]*model.Entry, error)
}

// NewDashboardHandler creates a new DashboardHandler
func NewDashboardHandler(entryRepo *repository.EntryRepository, personRepo *repository.PersonRepository, secureCookies bool) *DashboardHandler {
	return &DashboardHandler{
		entryRepo:     entryRepo,
		personRepo:    personRepo,
		secureCookies: secureCookies,
	}
}

// dashboardView is the signature shared by the full dashboard page and its
// HTMX partial.
type dashboardView func(groups []pages.GroupData, persons []*model.Person, currentGroup int, filter pages.RatingFilter, isAuthenticated bool) templ.Component

// DashboardPage renders the main dashboard with all groups
func (h *DashboardHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.DashboardPage)
}

// DashboardContent renders just the inner content for HTMX partial updates
func (h *DashboardHandler) DashboardContent(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.DashboardContent)
}

// render loads every entry in one query, groups it, and renders view. The
// current group, where new movies go by default, is the highest group
// number, or 1 when there are no entries. ?needs=<initial> limits the groups
// to movies that person has not rated and is remembered in meCookie; an
// empty ?needs= shows everything again.
func (h *DashboardHandler) render(w http.ResponseWriter, r *http.Request, view dashboardView) {
	ctx := r.Context()
	entries, err := h.entryRepo.ListAll(ctx)
	if err != nil {
		slog.Error("failed to get dashboard data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	persons, err := h.personRepo.GetAll(ctx)
	if err != nil {
		slog.Error("failed to get dashboard data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	groups := groupEntries(entries)
	currentGroup := 1
	if len(groups) > 0 {
		currentGroup = groups[0].Number
	}

	needs := h.needsPerson(w, r, persons)
	filter := newRatingFilter(entries, persons, needs)
	isAuthenticated := middleware.IsAuthenticated(ctx)
	view(groups, persons, currentGroup, filter, isAuthenticated).Render(ctx, w)
}

// needsPerson returns the person named by ?needs=, or by meCookie when the
// query has no needs parameter, or nil for everyone. A needs parameter
// updates meCookie: it remembers a known person and is cleared otherwise.
func (h *DashboardHandler) needsPerson(w http.ResponseWriter, r *http.Request, persons []*model.Person) *model.Person {
	if values, ok := r.URL.Query()["needs"]; ok {
		person := personByInitial(persons, values[0])
		cookie := &http.Cookie{
			Name:     meCookie,
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   h.secureCookies,
			SameSite: http.SameSiteLaxMode,
		}
		if person != nil {
			cookie.Value = url.QueryEscape(person.Initial)
			cookie.MaxAge = meCookieMaxAge
		}
		http.SetCookie(w, cookie)
		return person
	}

	cookie, err := r.Cookie(meCookie)
	if err != nil {
		return nil
	}
	initial, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return nil
	}
	return personByInitial(persons, initial)
}

// personByInitial finds the person with the given initial, ignoring case and
// surrounding space, or returns nil.
func personByInitial(persons []*model.Person, initial string) *model.Person {
	initial = strings.TrimSpace(initial)
	if initial == "" {
		return nil
	}
	for _, person := range persons {
		if strings.EqualFold(person.Initial, initial) {
			return person
		}
	}
	return nil
}

// newRatingFilter counts, for each person, the entries they have not rated,
// and keeps the people with at least one.
func newRatingFilter(entries []*model.Entry, persons []*model.Person, needs *model.Person) pages.RatingFilter {
	filter := pages.RatingFilter{Needs: needs}
	for _, person := range persons {
		count := 0
		for _, entry := range entries {
			if !entry.RatedBy(person.ID) {
				count++
			}
		}
		if count > 0 {
			filter.Waiting = append(filter.Waiting, pages.Waiting{Person: person, Count: count})
		}
	}
	return filter
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
