package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"

	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/google/uuid"
)

const (
	topMovieLimit = 5
	// minLoyalistPicks is how many genre-tagged picks someone needs before
	// they can be a Genre Loyalist, so one lucky pick cannot win the trophy.
	minLoyalistPicks = 3
	// minFavoriteGenreMovies is how many family-rated movies a genre needs
	// before it can be the family's highest-rated genre.
	minFavoriteGenreMovies = 2
	// minContrarianRatings is how many family-rated movies someone must rate
	// before they can be the Contrarian, so one odd score cannot win it.
	minContrarianRatings = 3
)

// StatsHandler handles the Trophy Room.
type StatsHandler struct {
	statsRepo *repository.StatsRepository
}

// NewStatsHandler creates a new StatsHandler.
func NewStatsHandler(statsRepo *repository.StatsRepository) *StatsHandler {
	return &StatsHandler{statsRepo: statsRepo}
}

// StatsPage renders the Trophy Room.
func (h *StatsHandler) StatsPage(w http.ResponseWriter, r *http.Request) {
	statsData, err := h.buildStatsData(r.Context())
	if err != nil {
		slog.Error("failed to build trophy room data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	if err := pages.StatsPage(statsData, isAuthenticated).Render(r.Context(), w); err != nil {
		slog.Error("failed to render trophy room", "error", err)
	}
}

func (h *StatsHandler) buildStatsData(ctx context.Context) (*model.StatsData, error) {
	advantageHolder, advantageGroup, err := h.statsRepo.GetAdvantageHolder(ctx)
	if err != nil {
		return nil, fmt.Errorf("get advantage holder: %w", err)
	}

	trophyStats, err := h.statsRepo.GetTrophyStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get trophy stats: %w", err)
	}

	topMovies, err := h.statsRepo.GetTopRatedMovies(ctx, topMovieLimit)
	if err != nil {
		return nil, fmt.Errorf("get top rated movies: %w", err)
	}

	genreStats, err := h.statsRepo.GetGenrePickStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get genre pick stats: %w", err)
	}

	tastePairs, err := h.statsRepo.GetTastePairs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get taste pairs: %w", err)
	}

	mostDivisive, err := h.statsRepo.GetMostDivisiveMovie(ctx)
	if err != nil {
		return nil, fmt.Errorf("get most divisive movie: %w", err)
	}

	totalWatched, totalRuntime, fullyRated, err := h.statsRepo.GetSummaryStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get summary stats: %w", err)
	}

	return &model.StatsData{
		AdvantageHolder:       advantageHolder,
		AdvantageGroup:        advantageGroup,
		Trophies:              calculateTrophies(trophyStats),
		TopMovies:             topMovies,
		Genres:                calculateGenreSpotlight(genreStats),
		TasteMatch:            calculateTasteMatch(trophyStats, tastePairs, mostDivisive),
		TotalMoviesWatched:    totalWatched,
		TotalWatchTimeMinutes: totalRuntime,
		FullyRatedMovies:      fullyRated,
	}, nil
}

func calculateTrophies(stats []model.TrophyStats) []model.Trophy {
	crowdWinners, crowdValue := maxWinners(stats, trophyStatsPerson,
		func(stat model.TrophyStats) bool { return stat.FullyRatedPicks > 0 },
		func(stat model.TrophyStats) float64 { return stat.AvgRatingReceived },
	)
	fanWinners, fanValue := maxWinners(stats, trophyStatsPerson,
		func(stat model.TrophyStats) bool { return stat.RatingsGiven > 0 },
		func(stat model.TrophyStats) float64 { return stat.AvgRatingGiven },
	)
	marathonWinners, marathonValue := maxWinners(stats, trophyStatsPerson,
		func(stat model.TrophyStats) bool { return stat.PicksWithKnownRuntime > 0 },
		func(stat model.TrophyStats) float64 { return float64(stat.TotalRuntimePicked) },
	)

	return []model.Trophy{
		{
			ID:          "crowd-pleaser",
			Title:       "Crowd Pleaser",
			Description: "Whose picks earn the biggest family cheers",
			Icon:        "popcorn",
			Winners:     crowdWinners,
			Value:       ratingValue(crowdWinners, crowdValue, "family average"),
		},
		{
			ID:          "biggest-fan",
			Title:       "Biggest Fan",
			Description: "The one who always finds something to love",
			Icon:        "star",
			Winners:     fanWinners,
			Value:       ratingValue(fanWinners, fanValue, "average rating"),
		},
		{
			ID:          "movie-marathoner",
			Title:       "Movie Marathoner",
			Description: "For keeping family movie night rolling",
			Icon:        "stopwatch",
			Winners:     marathonWinners,
			Value:       runtimeValue(marathonWinners, int(marathonValue)),
		},
	}
}

func maxWinners[T any](
	stats []T,
	person func(T) *model.Person,
	eligible func(T) bool,
	metric func(T) float64,
) ([]*model.Person, float64) {
	best := math.Inf(-1)
	var winners []*model.Person

	for _, stat := range stats {
		winner := person(stat)
		if winner == nil || !eligible(stat) {
			continue
		}
		value := metric(stat)
		switch {
		case value > best && !nearlyEqual(value, best):
			best = value
			winners = []*model.Person{winner}
		case nearlyEqual(value, best):
			winners = append(winners, winner)
		}
	}

	sort.Slice(winners, func(i, j int) bool { return winners[i].Name < winners[j].Name })
	if len(winners) == 0 {
		return nil, 0
	}
	return winners, best
}

func trophyStatsPerson(stat model.TrophyStats) *model.Person { return stat.Person }

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

func ratingValue(winners []*model.Person, value float64, label string) string {
	if len(winners) == 0 {
		return "Waiting for family ratings"
	}
	return fmt.Sprintf("%.1f %s", value, label)
}

func runtimeValue(winners []*model.Person, minutes int) string {
	if len(winners) == 0 {
		return "Waiting for movie picks"
	}
	return fmt.Sprintf("%s of picks", ui.FormatRuntime(minutes))
}

// calculateGenreSpotlight turns per-picker genre tallies into each person's
// most-picked genre, the genre the family rates highest, and the Genre
// Loyalist trophy for whoever concentrates their picks in one genre the most.
func calculateGenreSpotlight(stats []model.GenrePickStat) model.GenreSpotlight {
	type genreTotal struct {
		ratingTotal float64
		movies      int
	}
	var order []uuid.UUID
	favorites := map[uuid.UUID]model.PickerGenre{}
	totals := map[string]genreTotal{}

	for _, stat := range stats {
		total := totals[stat.Genre]
		total.ratingTotal += stat.RatingTotal
		total.movies += stat.RatedPicks
		totals[stat.Genre] = total

		if stat.Person == nil {
			continue
		}
		best, seen := favorites[stat.Person.ID]
		if !seen {
			order = append(order, stat.Person.ID)
		}
		if !seen || stat.Picks > best.Picks || (stat.Picks == best.Picks && stat.Genre < best.Genre) {
			favorites[stat.Person.ID] = model.PickerGenre{
				Person: stat.Person, Genre: stat.Genre, Picks: stat.Picks, TaggedPicks: stat.TaggedPicks,
			}
		}
	}

	pickerGenres := make([]model.PickerGenre, 0, len(order))
	for _, id := range order {
		pickerGenres = append(pickerGenres, favorites[id])
	}

	var familyFavorite *model.GenreRating
	for genre, total := range totals {
		if total.movies < minFavoriteGenreMovies {
			continue
		}
		candidate := model.GenreRating{
			Genre: genre, AverageRating: total.ratingTotal / float64(total.movies), Movies: total.movies,
		}
		if familyFavorite == nil || betterGenreRating(candidate, *familyFavorite) {
			familyFavorite = &candidate
		}
	}

	winners, share := maxWinners(pickerGenres,
		func(pick model.PickerGenre) *model.Person { return pick.Person },
		func(pick model.PickerGenre) bool { return pick.TaggedPicks >= minLoyalistPicks },
		func(pick model.PickerGenre) float64 { return float64(pick.Picks) / float64(pick.TaggedPicks) },
	)
	loyalist := model.Trophy{
		ID:          "genre-loyalist",
		Title:       "Genre Loyalist",
		Description: "Knows what they like and sticks with it",
		Icon:        "film-reel",
		Winners:     winners,
		Value:       fmt.Sprintf("Waiting for %d picks with a genre", minLoyalistPicks),
	}
	if len(winners) > 0 {
		loyalist.Value = fmt.Sprintf("%.0f%% of picks in one genre", share*100)
	}

	return model.GenreSpotlight{PickerGenres: pickerGenres, FamilyFavorite: familyFavorite, Loyalist: loyalist}
}

// calculateTasteMatch lays out each pair's rating agreement as the lower
// triangle of a matrix over the people in stats, in their order, and awards
// the Contrarian trophy to whoever strays furthest from the rest of the
// family's scores.
func calculateTasteMatch(stats []model.TrophyStats, pairs []model.TastePair, mostDivisive *model.DivisiveMovie) model.TasteMatch {
	// Index each pair under both orders so lookups need not sort the IDs.
	byPair := make(map[[2]uuid.UUID]*model.TastePair, 2*len(pairs))
	for i := range pairs {
		pair := &pairs[i]
		byPair[[2]uuid.UUID{pair.PersonA, pair.PersonB}] = pair
		byPair[[2]uuid.UUID{pair.PersonB, pair.PersonA}] = pair
	}

	var people []*model.Person
	for _, stat := range stats {
		if stat.Person != nil {
			people = append(people, stat.Person)
		}
	}

	var match model.TasteMatch
	if len(people) > 1 {
		match.Columns = people[:len(people)-1]
		for i, person := range people[1:] {
			row := model.TasteRow{Person: person, Cells: make([]*model.TastePair, i+1)}
			for j, other := range people[:i+1] {
				row.Cells[j] = byPair[[2]uuid.UUID{person.ID, other.ID}]
			}
			match.Rows = append(match.Rows, row)
		}
	}
	match.MostDivisive = mostDivisive

	winners, gap := maxWinners(stats, trophyStatsPerson,
		func(stat model.TrophyStats) bool { return stat.GapRatings >= minContrarianRatings },
		func(stat model.TrophyStats) float64 { return stat.AvgRatingGap },
	)
	match.Contrarian = model.Trophy{
		ID:          "contrarian",
		Title:       "Contrarian",
		Description: "Rates to the beat of their own drum",
		Icon:        "chart-up",
		Winners:     winners,
		Value:       fmt.Sprintf("Waiting for %d family-rated movies", minContrarianRatings),
	}
	if len(winners) > 0 {
		match.Contrarian.Value = fmt.Sprintf("%.1f points from the family, on average", gap)
	}

	return match
}

// betterGenreRating orders genres by average rating, then by how many movies
// back it up, then by name so the result never depends on map order.
func betterGenreRating(a, b model.GenreRating) bool {
	switch {
	case !nearlyEqual(a.AverageRating, b.AverageRating):
		return a.AverageRating > b.AverageRating
	case a.Movies != b.Movies:
		return a.Movies > b.Movies
	default:
		return a.Genre < b.Genre
	}
}
