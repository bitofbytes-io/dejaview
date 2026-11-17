package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestStatsPageRendersMonthlyPicks(t *testing.T) {
	daniel := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel", Color: "mint"}
	jennifer := &model.Person{ID: uuid.New(), Initial: "J", Name: "Jennifer", Color: "mint"}
	march := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	html := renderStatsPage(t, &model.StatsData{
		TotalMoviesWatched: 4,
		MonthlyPicks: []model.MonthlyPicks{
			{Month: march.AddDate(0, -1, 0)},
			{Month: march, Total: 4, Picks: []model.PickerCount{
				{Person: daniel, Count: 2},
				{Person: jennifer, Count: 1},
				{Count: 1},
			}},
		},
	})
	for _, want := range []string{
		"Picks Over Time",
		`aria-label="February 2026: no movies"`,
		`aria-label="March 2026: 4 movies (Daniel 2, Jennifer 1, No picker 1)"`,
		`<li class="picks-legend-item person-mint">`,
		`<li class="picks-legend-item picks-unpicked">`,
		"No picker <b>1</b>",
		`<span class="picks-month" aria-hidden="true">Mar 26</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Trophy Room did not contain %q", want)
		}
	}
	// Both pickers share a colour, so only the initial, title and count tell
	// their blocks apart.
	for _, block := range []struct {
		html string
		want int
	}{
		{`<span class="picks-unit person-mint" title="Daniel">D</span>`, 2},
		{`<span class="picks-unit person-mint" title="Jennifer">J</span>`, 1},
	} {
		if got := strings.Count(html, block.html); got != block.want {
			t.Errorf("found %d of %s, want %d", got, block.html, block.want)
		}
	}
	if got := strings.Count(html, `<span class="picks-unit picks-unpicked" title="No picker">?</span>`); got != 1 {
		t.Errorf("found %d unpicked blocks, want 1", got)
	}

	empty := renderStatsPage(t, &model.StatsData{TotalMoviesWatched: 1, MonthlyPicks: []model.MonthlyPicks{{Month: march}}})
	if !strings.Contains(empty, "No picks in the last year") || strings.Contains(empty, "picks-list") {
		t.Error("Trophy Room with no recent picks did not render the empty state")
	}
}
