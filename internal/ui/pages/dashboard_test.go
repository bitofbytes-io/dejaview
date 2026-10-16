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

	html := render(t, DashboardContent(groups, nil, 1, false))
	for _, unwanted := range addMovieControls {
		if strings.Contains(html, unwanted) {
			t.Errorf("logged-out dashboard contains %q", unwanted)
		}
	}
	if !strings.Contains(html, "Heat") {
		t.Error("logged-out dashboard does not list the movies")
	}

	html = render(t, DashboardContent(groups, nil, 1, true))
	for _, want := range addMovieControls {
		if !strings.Contains(html, want) {
			t.Errorf("logged-in dashboard is missing %q", want)
		}
	}
	if strings.Contains(html, "disabled") {
		t.Error("logged-in dashboard has disabled controls")
	}
}

func TestDashboardEmptyStateMatchesVisitor(t *testing.T) {
	if html := render(t, DashboardContent(nil, nil, 1, false)); strings.Contains(html, "Search for a movie above") {
		t.Error("logged-out empty state points at the hidden search")
	}
	if html := render(t, DashboardContent(nil, nil, 1, true)); !strings.Contains(html, "Search for a movie above") {
		t.Error("logged-in empty state lost its search hint")
	}
}
