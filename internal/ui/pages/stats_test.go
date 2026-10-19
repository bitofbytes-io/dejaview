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
	winner := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel", Color: "mint"}
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
		TopMovies: []model.RankedMovie{{Entry: entry, AverageRating: 9.2}},
		Genres: model.GenreSpotlight{
			PickerGenres:   []model.PickerGenre{{Person: winner, Genre: "Science Fiction", Picks: 3, TaggedPicks: 4}},
			FamilyFavorite: &model.GenreRating{Genre: "Comedy", AverageRating: 8.5, Movies: 2},
			Loyalist: model.Trophy{
				Title: "Genre Loyalist", Description: "Sticks with it", Icon: "film-reel",
				Winners: []*model.Person{winner}, Value: "75% of picks in one genre",
			},
		},
		TotalMoviesWatched:    1,
		TotalWatchTimeMinutes: 10040,
		FullyRatedMovies:      1,
	}

	html := renderStatsPage(t, data)
	for _, want := range []string{
		"Next Movie Night Bonus",
		"Family High Fives",
		"Crowd Pleaser",
		"Genre Spotlight",
		"Genre Loyalist",
		"75% of picks in one genre",
		"Family Favorite Genre",
		"Comedy",
		"8.5 family average over 2 movies",
		"Daniel: Science Fiction (3)",
		"Family Top Five",
		"The Family Favorite",
		"9.2",
		"/movies/" + entry.ID.String(),
		"167h 20m",
		`<span class="family-total-label">Movies Watched</span>`,
		`<span class="family-total-label">Movie Time Together</span>`,
		`<span class="family-total-label">Family-Rated Movies</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("populated Trophy Room did not contain %q", want)
		}
	}

	// Every person chip wears the person's colour: Crowd Pleaser, Genre
	// Loyalist, Go-To Genres and the Top Five picker.
	if got := strings.Count(html, `class="trophy-winner person-mint"`); got != 4 {
		t.Errorf("found %d person chips in Daniel's colour, want 4", got)
	}
	// Crowd Pleaser and all three genre and taste cards share the trophy card
	// layout, footer included, so their rows line up.
	if got := strings.Count(html, `<article class="trophy-card">`); got != 7 {
		t.Errorf("found %d trophy cards, want 7", got)
	}
	if got := strings.Count(html, `<div class="trophy-card-value">`); got != 7 {
		t.Errorf("found %d trophy card footers, want 7", got)
	}
	if got := strings.Count(html, `class="trophy-winners trophy-winners-stacked"`); got != 1 {
		t.Errorf("found %d stacked winner lists, want 1 for Go-To Genres", got)
	}
	if !strings.Contains(html, `<div class="top-movie-picker"><span class="sr-only">Picked by</span><span class="trophy-winner person-mint">`) {
		t.Error("Top Five did not show the picker as a person chip")
	}
}

func TestStatsPageRendersTasteMatch(t *testing.T) {
	aiden := &model.Person{ID: uuid.New(), Initial: "A", Name: "Aiden", Color: "violet"}
	caleb := &model.Person{ID: uuid.New(), Initial: "C", Name: "Caleb", Color: "amber"}
	daniel := &model.Person{ID: uuid.New(), Initial: "D", Name: "Daniel", Color: "cyan"}
	correlation := -0.25
	divisive := &model.DivisiveMovie{EntryID: uuid.New(), Title: "Cube", LowScore: 2, HighScore: 9.5}
	data := &model.StatsData{
		TotalMoviesWatched: 3,
		TasteMatch: model.TasteMatch{
			Columns: []*model.Person{aiden, caleb},
			Rows: []model.TasteRow{
				{Person: caleb, Cells: []*model.TastePair{{SharedMovies: 1, AvgGap: 0.5}}},
				{Person: daniel, Cells: []*model.TastePair{nil, {SharedMovies: 6, AvgGap: 2.5, Correlation: &correlation}}},
			},
			MostDivisive: divisive,
			Contrarian: model.Trophy{
				Title: "Contrarian", Icon: "chart-up",
				Winners: []*model.Person{daniel}, Value: "2.1 points from the family, on average",
			},
		},
	}

	html := renderStatsPage(t, data)
	for _, want := range []string{
		"Taste Match",
		"Who Rates Alike",
		`<th scope="col"><span class="trophy-winner-badge person-violet" title="Aiden" aria-hidden="true">A</span> <span class="sr-only">Aiden</span></th>`,
		`<th scope="row"><span class="trophy-winner-badge person-cyan" title="Daniel" aria-hidden="true">D</span>`,
		// Caleb and Aiden share one movie, so they are compared by gap.
		`<td class="taste-cell rating-high" title="1 shared movie, 0.5 points apart on average"><span class="taste-cell-value">±0.5</span></td>`,
		// Daniel and Aiden share nothing yet.
		`<td class="taste-cell taste-cell-empty" title="No shared movies yet">`,
		// Daniel and Caleb share six, so they are compared by correlation.
		`<td class="taste-cell rating-low" title="6 shared movies, 2.5 points apart on average"><span class="taste-cell-value">−0.25</span></td>`,
		"Most Divisive Movie",
		`href="/movies/` + divisive.EntryID.String() + `"`,
		">Cube</a>",
		"Scores from 2.0 to 9.5",
		"Contrarian",
		"2.1 points from the family, on average",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Taste Match did not contain %q", want)
		}
	}
	// Caleb's row pads the column he does not pair with.
	if got := strings.Count(html, "<td></td>"); got != 2 {
		t.Errorf("found %d blank cells, want the corner and one above the diagonal", got)
	}
}

func TestStatsPageWaitsForTasteMatch(t *testing.T) {
	html := renderStatsPage(t, &model.StatsData{TotalMoviesWatched: 1})
	for _, want := range []string{"Needs two people", "Waiting for a family-rated split"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty Taste Match did not contain %q", want)
		}
	}
	if strings.Contains(html, `<table class="taste-matrix">`) {
		t.Error("rendered a matrix without anyone to compare")
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
