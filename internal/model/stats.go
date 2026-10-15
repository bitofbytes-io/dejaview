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

// GenrePickStat is one picker's tally for one TMDB genre, read from the
// genres saved in movies.metadata_json. Person is nil for picks with no
// picker. A pick with several genres appears once per genre.
type GenrePickStat struct {
	Person *Person
	Genre  string
	// Picks counts this picker's entries tagged with Genre.
	Picks int
	// TaggedPicks counts this picker's entries that have any genre.
	TaggedPicks int
	// RatedPicks counts those Picks that every person rated, and RatingTotal
	// sums their family averages.
	RatedPicks  int
	RatingTotal float64
}

// PickerGenre is the genre one person has picked most often.
type PickerGenre struct {
	Person      *Person
	Genre       string
	Picks       int
	TaggedPicks int
}

// GenreRating is a genre's average family rating across fully rated movies.
type GenreRating struct {
	Genre         string
	AverageRating float64
	Movies        int
}

// GenreSpotlight holds the genre stats shown in the Trophy Room.
type GenreSpotlight struct {
	PickerGenres   []PickerGenre
	FamilyFavorite *GenreRating
	Loyalist       Trophy
}

// StatsData holds the streamlined data rendered by the Trophy Room.
type StatsData struct {
	AdvantageHolder *Person
	AdvantageGroup  int
	Trophies        []Trophy
	TopMovies       []RankedMovie
	Genres          GenreSpotlight

	TotalMoviesWatched    int
	TotalWatchTimeMinutes int
	FullyRatedMovies      int
}
