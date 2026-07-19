package model

// Trophy represents one friendly family movie-night award.
type Trophy struct {
	ID          string
	Title       string
	Description string
	Icon        string
	Winners     []*Person
	Value       string
}

// TrophyStats contains the simple per-person metrics used to award trophies.
type TrophyStats struct {
	Person                *Person
	AvgRatingGiven        float64
	RatingsGiven          int
	AvgRatingReceived     float64
	FullyRatedPicks       int
	TotalRuntimePicked    int
	PicksWithKnownRuntime int
}

// RankedMovie is a fully rated entry in the family's top-five list.
type RankedMovie struct {
	Entry         *Entry
	AverageRating float64
}

// StatsData holds the streamlined data rendered by the Trophy Room.
type StatsData struct {
	AdvantageHolder *Person
	AdvantageGroup  int
	Trophies        []Trophy
	TopMovies       []RankedMovie

	TotalMoviesWatched    int
	TotalWatchTimeMinutes int
	FullyRatedMovies      int
}
