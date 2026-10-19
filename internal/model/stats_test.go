package model

import "testing"

func TestTrophyRatingThreshold(t *testing.T) {
	for people, want := range map[int]int{0: 1, 1: 1, 2: 1, 3: 2, 4: 3, 5: 4} {
		if got := TrophyRatingThreshold(people); got != want {
			t.Errorf("TrophyRatingThreshold(%d) = %d, want %d", people, got, want)
		}
	}
}

func TestTastePairAgreement(t *testing.T) {
	r := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name        string
		pair        TastePair
		correlation bool
		class       string
	}{
		{"close gap", TastePair{SharedMovies: 2, AvgGap: 1}, false, "rating-high"},
		{"middling gap under 5 shared", TastePair{SharedMovies: 4, AvgGap: 2, Correlation: r(0.9)}, false, "rating-mid"},
		{"wide gap", TastePair{SharedMovies: 1, AvgGap: 3.5}, false, "rating-low"},
		{"undefined correlation", TastePair{SharedMovies: 5, AvgGap: 0.5}, false, "rating-high"},
		{"strong correlation", TastePair{SharedMovies: 5, AvgGap: 3, Correlation: r(0.5)}, true, "rating-high"},
		{"weak correlation", TastePair{SharedMovies: 8, AvgGap: 0.5, Correlation: r(0)}, true, "rating-mid"},
		{"opposite tastes", TastePair{SharedMovies: 8, AvgGap: 1, Correlation: r(-0.4)}, true, "rating-low"},
	} {
		if got := tc.pair.UsesCorrelation(); got != tc.correlation {
			t.Errorf("%s: UsesCorrelation = %v, want %v", tc.name, got, tc.correlation)
		}
		if got := tc.pair.AgreementClass(); got != tc.class {
			t.Errorf("%s: AgreementClass = %q, want %q", tc.name, got, tc.class)
		}
	}
}
