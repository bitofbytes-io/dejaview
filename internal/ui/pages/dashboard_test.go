package pages

import (
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// The Add movie panel, which visitors who aren't logged in never see.
var addMovieControls = []string{
	"<span>Add movie</span>",
	`id="movie-search"`,
	`id="add-group-select"`,
	"/api/tmdb/search",
	`id="search-results"`,
}

func TestDashboardHidesAddMovieWhenLoggedOut(t *testing.T) {
	groups := []GroupData{{Number: 1, Entries: []*model.Entry{
		{ID: uuid.New(), GroupNumber: 1, Movie: &model.Movie{Title: "Heat"}},
	}}}

	html := render(t, DashboardContent(groups, nil, 1, RatingFilter{}, false))
	for _, unwanted := range addMovieControls {
		if strings.Contains(html, unwanted) {
			t.Errorf("logged-out dashboard contains %q", unwanted)
		}
	}
	if !strings.Contains(html, "Heat") {
		t.Error("logged-out dashboard does not list the movies")
	}

	html = render(t, DashboardContent(groups, nil, 1, RatingFilter{}, true))
	for _, want := range addMovieControls {
		if !strings.Contains(html, want) {
			t.Errorf("logged-in dashboard is missing %q", want)
		}
	}
	if strings.Contains(html, "disabled") {
		t.Error("logged-in dashboard has disabled controls")
	}
}

func TestDashboardGroupCountShowsTotal(t *testing.T) {
	entries := func(group, n int) []*model.Entry {
		out := make([]*model.Entry, n)
		for i := range out {
			out[i] = &model.Entry{ID: uuid.New(), GroupNumber: group, Movie: &model.Movie{Title: "Heat"}}
		}
		return out
	}
	groups := []GroupData{
		{Number: 1, Entries: entries(1, 8)},
		{Number: 2, Entries: entries(2, 9)},
		{Number: 3, Entries: entries(3, 5)},
	}

	for _, authed := range []bool{false, true} {
		html := render(t, DashboardContent(groups, nil, 3, RatingFilter{}, authed))
		for _, want := range []string{"8 out of 8", "9 out of 9", "5 out of 9"} {
			if !strings.Contains(html, want) {
				t.Errorf("dashboard (authenticated=%v) is missing group count %q", authed, want)
			}
		}
	}
}

func TestDashboardEmptyStateMatchesVisitor(t *testing.T) {
	if html := render(t, DashboardContent(nil, nil, 1, RatingFilter{}, false)); strings.Contains(html, "Search for a movie above") {
		t.Error("logged-out empty state points at the hidden search")
	}
	if html := render(t, DashboardContent(nil, nil, 1, RatingFilter{}, true)); !strings.Contains(html, "Search for a movie above") {
		t.Error("logged-in empty state lost its search hint")
	}
}

func TestPosterShowsWhoHasNotRated(t *testing.T) {
	persons := []*model.Person{
		{ID: uuid.New(), Initial: "A", Name: "Ana", Color: "rose"},
		{ID: uuid.New(), Initial: "B", Name: "Ben", Color: "mint"},
	}
	entry := &model.Entry{ID: uuid.New(), GroupNumber: 1, Movie: &model.Movie{Title: "Heat"}, Ratings: []*model.Rating{
		{PersonID: persons[0].ID, Score: 8, Person: persons[0]},
	}}
	groups := []GroupData{{Number: 1, Entries: []*model.Entry{entry}}}

	html := render(t, DashboardContent(groups, persons, 1, RatingFilter{}, false))
	if !strings.Contains(html, `class="rating-badge rating-missing person-mint" title="Waiting on Ben"`) {
		t.Errorf("poster does not show Ben is missing:\n%s", html)
	}
	if strings.Contains(html, "Waiting on Ana") {
		t.Errorf("poster shows Ana as missing after she rated:\n%s", html)
	}
}

func TestDashboardFilteredGroupsAreNotSortable(t *testing.T) {
	ana := &model.Person{ID: uuid.New(), Initial: "A", Name: "Ana"}
	groups := []GroupData{{Number: 1, Entries: []*model.Entry{
		{ID: uuid.New(), GroupNumber: 1, Movie: &model.Movie{Title: "Heat"}},
	}}}
	filter := RatingFilter{Needs: ana, Waiting: []Waiting{{Person: ana, Count: 1}}}

	html := render(t, DashboardContent(groups, []*model.Person{ana}, 1, filter, true))
	if strings.Contains(html, "sortable-grid") || strings.Contains(html, "draggable-item") {
		t.Errorf("filtered dashboard can be reordered:\n%s", html)
	}
	if html := render(t, DashboardContent(groups, []*model.Person{ana}, 1, RatingFilter{}, true)); !strings.Contains(html, "sortable-grid") {
		t.Errorf("unfiltered dashboard lost reordering:\n%s", html)
	}
}
