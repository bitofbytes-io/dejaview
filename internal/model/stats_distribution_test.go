package model

import "testing"

func TestRatingDistributionPeaks(t *testing.T) {
	var empty RatingDistribution
	if empty.MostCommon() != 0 || empty.MostInBucket() != 0 {
		t.Errorf("empty distribution: most common %d, fullest %d, want 0 and 0", empty.MostCommon(), empty.MostInBucket())
	}

	dist := RatingDistribution{Count: 7}
	dist.Buckets[3], dist.Buckets[7], dist.Buckets[9] = 3, 2, 2
	if got := dist.MostCommon(); got != 3 {
		t.Errorf("MostCommon = %d, want 3", got)
	}
	if got := dist.MostInBucket(); got != 3 {
		t.Errorf("MostInBucket = %d, want 3", got)
	}

	// Ties go to the higher score.
	dist.Buckets[9] = 3
	if got := dist.MostCommon(); got != 9 {
		t.Errorf("tied MostCommon = %d, want 9", got)
	}

	zero := RatingDistribution{Count: 2}
	zero.Buckets[0] = 2
	if got := zero.MostCommon(); got != 0 {
		t.Errorf("all-zero MostCommon = %d, want 0", got)
	}
}
