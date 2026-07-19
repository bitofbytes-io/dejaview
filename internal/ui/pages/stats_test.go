package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestStatsPageRendersFriendlyEmptyState(t *testing.T) {
	html := renderStatsPage(t, &model.StatsData{})

	for _, want := range []string{"Family Trophy Room", "The show starts soon!", "Find Our First Movie", "Trophy Room"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty Trophy Room did not contain %q", want)
		}
	}
	if strings.Contains(html, "egos are crushed") {
		t.Error("empty Trophy Room retained the old hostile copy")
	}
}

func TestStatsPageRendersTrophiesAndRankedMovies(t *testing.T) {
	winner := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel"}
	entry := &model.Entry{
		ID:             uuid.New(),
		GroupNumber:    2,
		Movie:          &model.Movie{ID: uuid.New(), Title: "The Family Favorite"},
		PickedByPerson: winner,
	}
	data := &model.StatsData{
		AdvantageHolder: winner,
		AdvantageGroup:  1,
		Trophies: []model.Trophy{{
			Title: "Crowd Pleaser", Description: "Big cheers", Icon: "popcorn",
			Winners: []*model.Person{winner}, Value: "9.2 family average",
		}},
		TopMovies:          []model.RankedMovie{{Entry: entry, AverageRating: 9.2}},
		TotalMoviesWatched: 1,
		FullyRatedMovies:   1,
	}

	html := renderStatsPage(t, data)
	for _, want := range []string{
		"Next Movie Night Bonus",
		"Family High Fives",
		"Crowd Pleaser",
		"Family Top Five",
		"The Family Favorite",
		"9.2",
		"/movies/" + entry.ID.String(),
	} {
		if !strings.Contains(html, want) {
			t.Errorf("populated Trophy Room did not contain %q", want)
		}
	}
}

func renderStatsPage(t *testing.T, data *model.StatsData) string {
	t.Helper()
	var output bytes.Buffer
	if err := StatsPage(data, false).Render(context.Background(), &output); err != nil {
		t.Fatalf("render StatsPage: %v", err)
	}
	return output.String()
}
