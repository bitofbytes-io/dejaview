package model

import "testing"

func TestTrophyRatingThreshold(t *testing.T) {
	for people, want := range map[int]int{0: 1, 1: 1, 2: 1, 3: 2, 4: 3, 5: 4} {
		if got := TrophyRatingThreshold(people); got != want {
			t.Errorf("TrophyRatingThreshold(%d) = %d, want %d", people, got, want)
		}
	}
}
