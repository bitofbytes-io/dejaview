package handler

import (
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestCalculateTrophiesSelectsSimplePositiveWinners(t *testing.T) {
	daniel := testPerson("Daniel", "D")
	jennifer := testPerson("Jennifer", "J")
	caleb := testPerson("Caleb", "C")

	trophies := calculateTrophies([]model.TrophyStats{
		{
			Person: daniel, AvgRatingReceived: 8.4, FullyRatedPicks: 2,
			AvgRatingGiven: 7.1, RatingsGiven: 8,
			TotalRuntimePicked: 250, PicksWithKnownRuntime: 2,
		},
		{
			Person: jennifer, AvgRatingReceived: 7.8, FullyRatedPicks: 2,
			AvgRatingGiven: 8.8, RatingsGiven: 8,
			TotalRuntimePicked: 190, PicksWithKnownRuntime: 2,
		},
		{
			Person: caleb, AvgRatingReceived: 8.0, FullyRatedPicks: 1,
			AvgRatingGiven: 7.9, RatingsGiven: 4,
			TotalRuntimePicked: 320, PicksWithKnownRuntime: 3,
		},
	})

	if len(trophies) != 3 {
		t.Fatalf("got %d trophies, want 3", len(trophies))
	}
	assertTrophyWinner(t, trophies[0], "Crowd Pleaser", daniel, "8.4 family average")
	assertTrophyWinner(t, trophies[1], "Biggest Fan", jennifer, "8.8 average rating")
	assertTrophyWinner(t, trophies[2], "Movie Marathoner", caleb, "5h 20m of picks")
}

func TestCalculateTrophiesSharesExactTies(t *testing.T) {
	aiden := testPerson("Aiden", "A")
	caleb := testPerson("Caleb", "C")

	trophies := calculateTrophies([]model.TrophyStats{
		{Person: caleb, AvgRatingReceived: 8.25, FullyRatedPicks: 1},
		{Person: aiden, AvgRatingReceived: 8.25, FullyRatedPicks: 1},
	})

	winners := trophies[0].Winners
	if len(winners) != 2 {
		t.Fatalf("got %d tied winners, want 2", len(winners))
	}
	if winners[0] != aiden || winners[1] != caleb {
		t.Fatalf("winners were not sorted by name: %#v", winners)
	}
}

func TestCalculateTrophiesLeavesIneligibleAwardsOpen(t *testing.T) {
	trophies := calculateTrophies([]model.TrophyStats{{Person: testPerson("Daniel", "D")}})

	for _, trophy := range trophies {
		if len(trophy.Winners) != 0 {
			t.Errorf("%s unexpectedly had a winner", trophy.Title)
		}
		if trophy.Value == "" {
			t.Errorf("%s should explain how to unlock it", trophy.Title)
		}
	}
}

func TestCalculateGenreSpotlight(t *testing.T) {
	daniel := testPerson("Daniel", "D")
	jennifer := testPerson("Jennifer", "J")

	spotlight := calculateGenreSpotlight([]model.GenrePickStat{
		{Person: daniel, Genre: "Drama", Picks: 1, TaggedPicks: 4, RatedPicks: 1, RatingTotal: 9},
		{Person: daniel, Genre: "Horror", Picks: 3, TaggedPicks: 4, RatedPicks: 2, RatingTotal: 12},
		{Person: jennifer, Genre: "Comedy", Picks: 2, TaggedPicks: 3, RatedPicks: 1, RatingTotal: 6},
		{Person: jennifer, Genre: "Drama", Picks: 2, TaggedPicks: 3, RatedPicks: 2, RatingTotal: 15},
		{Person: nil, Genre: "Drama", Picks: 1, TaggedPicks: 1, RatedPicks: 1, RatingTotal: 9},
	})

	if len(spotlight.PickerGenres) != 2 {
		t.Fatalf("got %d picker genres, want 2: %+v", len(spotlight.PickerGenres), spotlight.PickerGenres)
	}
	if pick := spotlight.PickerGenres[0]; pick.Person != daniel || pick.Genre != "Horror" || pick.Picks != 3 {
		t.Errorf("Daniel's genre = %+v, want Horror x3", pick)
	}
	// Jennifer's Comedy and Drama tie on picks, so the name breaks the tie.
	if pick := spotlight.PickerGenres[1]; pick.Person != jennifer || pick.Genre != "Comedy" {
		t.Errorf("Jennifer's genre = %+v, want Comedy", pick)
	}

	// Drama averages (9+15+9)/4 = 8.25 across four movies; Horror is 6.
	favorite := spotlight.FamilyFavorite
	if favorite == nil || favorite.Genre != "Drama" || favorite.Movies != 4 || !nearlyEqual(favorite.AverageRating, 8.25) {
		t.Errorf("family favorite = %+v, want Drama 8.25 over 4 movies", favorite)
	}

	// Daniel put 3 of 4 picks in Horror (75%), Jennifer 2 of 3 in a genre (67%).
	assertTrophyWinner(t, spotlight.Loyalist, "Genre Loyalist", daniel, "75% of picks in one genre")
}

func TestCalculateGenreSpotlightWaitsForEnoughData(t *testing.T) {
	daniel := testPerson("Daniel", "D")

	empty := calculateGenreSpotlight(nil)
	if len(empty.PickerGenres) != 0 || empty.FamilyFavorite != nil || len(empty.Loyalist.Winners) != 0 || empty.Loyalist.Value == "" {
		t.Errorf("empty spotlight = %+v", empty)
	}

	// One rated movie is too few to name a favorite genre, and two tagged
	// picks are too few for the loyalist trophy.
	sparse := calculateGenreSpotlight([]model.GenrePickStat{
		{Person: daniel, Genre: "Drama", Picks: 2, TaggedPicks: 2, RatedPicks: 1, RatingTotal: 9},
	})
	if sparse.FamilyFavorite != nil {
		t.Errorf("family favorite = %+v, want none", sparse.FamilyFavorite)
	}
	if len(sparse.Loyalist.Winners) != 0 {
		t.Errorf("loyalist winners = %+v, want none", sparse.Loyalist.Winners)
	}
	if len(sparse.PickerGenres) != 1 || sparse.PickerGenres[0].Genre != "Drama" {
		t.Errorf("picker genres = %+v, want Daniel's Drama", sparse.PickerGenres)
	}
}

func TestCalculateGenreSpotlightSharesLoyalistTies(t *testing.T) {
	aiden := testPerson("Aiden", "A")
	caleb := testPerson("Caleb", "C")

	spotlight := calculateGenreSpotlight([]model.GenrePickStat{
		{Person: caleb, Genre: "Western", Picks: 3, TaggedPicks: 3},
		{Person: aiden, Genre: "Animation", Picks: 4, TaggedPicks: 4},
	})

	winners := spotlight.Loyalist.Winners
	if len(winners) != 2 || winners[0] != aiden || winners[1] != caleb {
		t.Fatalf("loyalist winners = %+v, want Aiden and Caleb", winners)
	}
	if spotlight.Loyalist.Value != "100% of picks in one genre" {
		t.Errorf("value = %q", spotlight.Loyalist.Value)
	}
}

func TestCalculateTasteMatchBuildsLowerTriangle(t *testing.T) {
	aiden := testPerson("Aiden", "A")
	caleb := testPerson("Caleb", "C")
	daniel := testPerson("Daniel", "D")
	stats := []model.TrophyStats{{Person: aiden}, {Person: caleb}, {Person: daniel}}
	// Pairs may come in either order; Aiden and Daniel share no movies.
	pairs := []model.TastePair{
		{PersonA: caleb.ID, PersonB: aiden.ID, SharedMovies: 2, AvgGap: 1.5},
		{PersonA: daniel.ID, PersonB: caleb.ID, SharedMovies: 6, AvgGap: 0.5},
	}
	divisive := &model.DivisiveMovie{Title: "Cube", LowScore: 2, HighScore: 9}

	match := calculateTasteMatch(stats, pairs, divisive)

	if len(match.Columns) != 2 || match.Columns[0] != aiden || match.Columns[1] != caleb {
		t.Fatalf("columns = %+v, want Aiden and Caleb", match.Columns)
	}
	if len(match.Rows) != 2 || match.Rows[0].Person != caleb || match.Rows[1].Person != daniel {
		t.Fatalf("rows = %+v, want Caleb and Daniel", match.Rows)
	}
	if cells := match.Rows[0].Cells; len(cells) != 1 || cells[0] == nil || cells[0].AvgGap != 1.5 {
		t.Errorf("Caleb's row = %+v, want only Aiden at 1.5", cells)
	}
	if cells := match.Rows[1].Cells; len(cells) != 2 || cells[0] != nil || cells[1] == nil || cells[1].SharedMovies != 6 {
		t.Errorf("Daniel's row = %+v, want no Aiden pair and the Caleb pair", cells)
	}
	if match.MostDivisive != divisive {
		t.Errorf("most divisive = %+v, want %+v", match.MostDivisive, divisive)
	}
}

func TestCalculateTasteMatchNeedsTwoPeople(t *testing.T) {
	match := calculateTasteMatch([]model.TrophyStats{{Person: testPerson("Daniel", "D")}}, nil, nil)
	if len(match.Columns) != 0 || len(match.Rows) != 0 {
		t.Errorf("one person built a matrix: %+v", match)
	}
	if len(match.Contrarian.Winners) != 0 || match.Contrarian.Value != "Waiting for 3 family-rated movies" {
		t.Errorf("contrarian = %+v, want an open trophy", match.Contrarian)
	}
}

func TestCalculateTasteMatchContrarian(t *testing.T) {
	aiden := testPerson("Aiden", "A")
	caleb := testPerson("Caleb", "C")
	daniel := testPerson("Daniel", "D")
	jennifer := testPerson("Jennifer", "J")

	// Aiden strays furthest but has rated too few family-rated movies.
	match := calculateTasteMatch([]model.TrophyStats{
		{Person: aiden, AvgRatingGap: 4, GapRatings: 2},
		{Person: caleb, AvgRatingGap: 2.4, GapRatings: 3},
		{Person: daniel, AvgRatingGap: 1, GapRatings: 5},
		{Person: jennifer, AvgRatingGap: 2.4, GapRatings: 4},
	}, nil, nil)

	winners := match.Contrarian.Winners
	if len(winners) != 2 || winners[0] != caleb || winners[1] != jennifer {
		t.Fatalf("contrarian winners = %+v, want Caleb and Jennifer", winners)
	}
	if match.Contrarian.Title != "Contrarian" || match.Contrarian.Value != "2.4 points from the family, on average" {
		t.Errorf("contrarian = %+v", match.Contrarian)
	}
}

func testPerson(name, initial string) *model.Person {
	return &model.Person{ID: uuid.New(), Name: name, Initial: initial}
}

func assertTrophyWinner(t *testing.T, trophy model.Trophy, title string, winner *model.Person, value string) {
	t.Helper()
	if trophy.Title != title {
		t.Errorf("title = %q, want %q", trophy.Title, title)
	}
	if len(trophy.Winners) != 1 || trophy.Winners[0] != winner {
		t.Errorf("winners = %#v, want only %s", trophy.Winners, winner.Name)
	}
	if trophy.Value != value {
		t.Errorf("value = %q, want %q", trophy.Value, value)
	}
}
