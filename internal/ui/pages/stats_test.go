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
	if !strings.Contains(html, `<div class="top-movie-picker"><span class="sr-only">Picked by</span><span class="trophy-winner person-mint">`) {
		t.Error("Top Five did not show the picker as a person chip")
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
