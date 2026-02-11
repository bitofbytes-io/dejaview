package model

import "github.com/google/uuid"

// PersonStats aggregates all statistics for a single person
type PersonStats struct {
	Person                *Person
	TotalPicks            int     // number of movies they've picked
	MoviesRated           int     // movies they've rated
	AvgRatingGiven        float64 // average rating they give to others' picks
	AvgRatingReceived     float64 // average rating their picks receive
	FirstPickCount        int     // times their movie was in position 1 (first to watch)
	LastPickCount         int     // times their movie was in last position
	RatingStdDev          float64 // standard deviation of their ratings (consistency)
	AvgDeviationFromGroup float64 // how far their ratings deviate from group average
	SelfLowestCount       int     // times they rated their own pick lowest in the family
	TotalRuntimePicked    int     // total runtime of movies they picked (minutes)
	AvgReleaseYear        float64 // average release year of their picks
	AvgRuntimePerPick     float64 // average runtime per movie they picked
	MinReleaseYear        int     // earliest release year of their picks
	MaxReleaseYear        int     // latest release year of their picks
	AvgPickVariance       float64 // average rating variance of their picks (how divisive)
	SelfInflationCount    int     // times they rated their own pick higher than group avg
	LastPickAvgRating     float64 // average rating received on last picks specifically
}

// Award represents a silly superlative award
type Award struct {
	ID          string  // "headliner", "corporate_darling", etc.
	Title       string  // "The Headliner"
	Description string  // Fun explanation/tagline
	Icon        string  // Emoji
	Winner      *Person // Current holder (nil if none qualify)
	Value       string  // "5 first picks", "8.2 avg"
}

// MovieAward represents an award for a specific movie
type MovieAward struct {
	ID          string // "hype_train", "unifier", etc.
	Title       string // "The Hype Train"
	Description string // Fun explanation
	Icon        string // Emoji
	Movie       *Movie // The winning movie
	Entry       *Entry // The entry (for picker info)
	Value       string // "Spread: 4.2"
}

// LeaderboardEntry represents one row in a leaderboard
type LeaderboardEntry struct {
	Person *Person
	Value  float64
	Label  string // formatted value like "7.8"
}

// Leaderboard represents a ranked list
type Leaderboard struct {
	Title    string
	Icon     string
	Entries  []LeaderboardEntry
	MaxValue float64 // for calculating bar widths
}

// StatsData holds all data needed to render the stats page
type StatsData struct {
	// The 3-pick advantage holder
	AdvantageHolder *Person
	AdvantageGroup  int // which group gave them the advantage

	// Person awards
	Awards []Award

	// Movie awards
	MovieAwards []MovieAward

	// Leaderboards
	Leaderboards []Leaderboard

	// Per-person detailed stats
	PersonStats []PersonStats

	// Summary stats
	TotalMoviesWatched    int
	TotalWatchTimeMinutes int
	TotalGroups           int
	FullyRatedMovies      int // movies with all 4 ratings
}

// MovieWithStats holds a movie with its rating statistics
type MovieWithStats struct {
	Entry        *Entry
	Movie        *Movie
	AvgRating    float64
	RatingStdDev float64
	Picker       *Person
}

// PickPositionStats holds first/last pick counts per person
type PickPositionStats struct {
	PersonID       uuid.UUID
	FirstPickCount int
	LastPickCount  int
}

// RatingStats holds rating statistics per person
type RatingStats struct {
	PersonID          uuid.UUID
	AvgRatingGiven    float64
	AvgRatingReceived float64
	RatingStdDev      float64
	TotalRatingsGiven int
}

// DeviationStats holds how much a person deviates from group average
type DeviationStats struct {
	PersonID     uuid.UUID
	AvgDeviation float64
}

// SelfRatingStats holds how often someone rates their own pick lowest
type SelfRatingStats struct {
	PersonID        uuid.UUID
	SelfLowestCount int
}

// PickMetadataStats holds runtime and release year stats per person
type PickMetadataStats struct {
	PersonID        uuid.UUID
	TotalRuntime    int
	AvgReleaseYear  float64
	AvgRuntime      float64
	MinReleaseYear  int
	MaxReleaseYear  int
	PickCount       int
	AvgPickVariance float64
}

// SelfInflationStats holds how often someone rates their own pick above group average
type SelfInflationStats struct {
	PersonID           uuid.UUID
	SelfInflationCount int
}

// LastPickRatingStats holds avg rating received on last picks per person
type LastPickRatingStats struct {
	PersonID          uuid.UUID
	LastPickAvgRating float64
}
