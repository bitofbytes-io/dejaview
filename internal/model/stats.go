package model

import "github.com/google/uuid"

// TrophyRatingThreshold returns how many people must rate a movie before it
// counts toward the Trophy Room: everyone but one, and at least one. With four
// people that is three. The stats queries apply the same rule in SQL.
func TrophyRatingThreshold(people int) int {
	return max(people-1, 1)
}

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
	// AvgRatingGap is how far, on average, this person's scores sit from the
	// average of everyone else's scores on the same family-rated movie;
	// GapRatings counts those scores.
	AvgRatingGap float64
	GapRatings   int
}

// MinCorrelationMovies is how many movies two people must both rate before
// the Taste Match shows the correlation of their scores instead of the
// average gap between them.
const MinCorrelationMovies = 5

// TastePair is how closely two people's ratings agree on the movies they
// have both rated.
type TastePair struct {
	PersonA      uuid.UUID
	PersonB      uuid.UUID
	SharedMovies int
	// AvgGap is the mean absolute difference between their scores.
	AvgGap float64
	// Correlation is the Pearson correlation of their scores, or nil when it
	// is undefined because one of them gave every shared movie the same score.
	Correlation *float64
}

// UsesCorrelation reports whether the pair shares enough movies, with enough
// variety, to compare them by correlation rather than by average gap.
func (p *TastePair) UsesCorrelation() bool {
	return p.SharedMovies >= MinCorrelationMovies && p.Correlation != nil
}

// AgreementClass returns the rating colour class for how well the pair
// agrees: close or positively correlated scores are high, far apart or
// opposite scores are low.
func (p *TastePair) AgreementClass() string {
	if p.UsesCorrelation() {
		switch {
		case *p.Correlation >= 0.5:
			return "rating-high"
		case *p.Correlation >= 0:
			return "rating-mid"
		default:
			return "rating-low"
		}
	}
	switch {
	case p.AvgGap <= 1:
		return "rating-high"
	case p.AvgGap <= 2:
		return "rating-mid"
	default:
		return "rating-low"
	}
}

// TasteRow is one person's row of the Taste Match matrix. Cells[i] is their
// pair with TasteMatch.Columns[i], or nil when the two share no rated movie.
type TasteRow struct {
	Person *Person
	Cells  []*TastePair
}

// DivisiveMovie is the family-rated movie whose scores spread the widest.
type DivisiveMovie struct {
	EntryID   uuid.UUID
	Title     string
	LowScore  float64
	HighScore float64
}

// TasteMatch holds the Trophy Room's rating-agreement stats. The matrix is
// the lower triangle: each row's person against every column person before
// them, so each pair appears once.
type TasteMatch struct {
	Columns      []*Person
	Rows         []TasteRow
	MostDivisive *DivisiveMovie
	Contrarian   Trophy
}

// RankedMovie is a family-rated entry in the family's top-five list.
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
	// RatedPicks counts those Picks rated by enough people to count (see
	// TrophyRatingThreshold), and RatingTotal
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

// GenreRating is a genre's average family rating across family-rated movies.
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
	TasteMatch      TasteMatch

	TotalMoviesWatched    int
	TotalWatchTimeMinutes int
	FullyRatedMovies      int
}
