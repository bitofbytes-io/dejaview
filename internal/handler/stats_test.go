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
