package pages

import (
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestStatsPageRendersRatingDistributions(t *testing.T) {
	daniel := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel", Color: "mint"}
	jennifer := &model.Person{ID: uuid.New(), Initial: "J", Name: "Jennifer", Color: "rose"}
	danielDist := model.RatingDistribution{Person: daniel, Count: 4, Mean: 7.5}
	danielDist.Buckets[6], danielDist.Buckets[8] = 1, 3
	jenniferDist := model.RatingDistribution{Person: jennifer, Count: 1, Mean: 3}
	jenniferDist.Buckets[3] = 1

	html := renderStatsPage(t, &model.StatsData{
		TotalMoviesWatched: 2,
		Distributions:      []model.RatingDistribution{danielDist, jenniferDist},
	})
	for _, want := range []string{
		"How Everyone Rates",
		`<li class="distribution-row person-mint">`,
		`<li class="distribution-row person-rose">`,
		"4 ratings · most often 8",
		"1 rating · most often 3",
		"7.5",
		"3 ratings of 8",
		"1 rating of 6",
		"0 ratings of 0",
		"distribution-bar-12",
		"distribution-bar-4",
		"distribution-bar-0",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Trophy Room did not contain %q", want)
		}
	}
	if got := strings.Count(html, `class="distribution-row `); got != 2 {
		t.Errorf("found %d distribution rows, want 2", got)
	}

	empty := renderStatsPage(t, &model.StatsData{TotalMoviesWatched: 2})
	if strings.Contains(empty, `class="distribution-list"`) || !strings.Contains(empty, "No ratings yet") {
		t.Error("Trophy Room without ratings should show the empty state, not a distribution list")
	}
}
