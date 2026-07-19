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

func TestFormatRuntime(t *testing.T) {
	tests := map[int]string{
		0:   "0m",
		45:  "45m",
		60:  "1h",
		125: "2h 5m",
	}
	for minutes, want := range tests {
		if got := formatRuntime(minutes); got != want {
			t.Errorf("formatRuntime(%d) = %q, want %q", minutes, got, want)
		}
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
