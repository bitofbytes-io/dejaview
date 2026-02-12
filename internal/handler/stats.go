package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/google/uuid"
)

// StatsHandler handles the statistics dashboard
type StatsHandler struct {
	statsRepo *repository.StatsRepository
}

// NewStatsHandler creates a new StatsHandler
func NewStatsHandler(statsRepo *repository.StatsRepository) *StatsHandler {
	return &StatsHandler{
		statsRepo: statsRepo,
	}
}

// StatsPage renders the statistics dashboard
func (h *StatsHandler) StatsPage(w http.ResponseWriter, r *http.Request) {
	statsData, err := h.buildStatsData(r.Context())
	if err != nil {
		slog.Error("failed to build stats data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	pages.StatsPage(statsData).Render(r.Context(), w)
}

// buildStatsData aggregates all statistics and calculates awards
func (h *StatsHandler) buildStatsData(ctx context.Context) (*model.StatsData, error) {
	// Get all persons for lookup
	persons, err := h.statsRepo.GetAllPersons(ctx)
	if err != nil {
		return nil, fmt.Errorf("get persons: %w", err)
	}

	// Get current group
	currentGroup, err := h.statsRepo.GetCurrentGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current group: %w", err)
	}

	// Get advantage holder
	advantageHolder, advantageGroup, err := h.statsRepo.GetAdvantageHolder(ctx, currentGroup)
	if err != nil {
		return nil, fmt.Errorf("get advantage holder: %w", err)
	}

	// Get all the raw stats
	pickPositionStats, err := h.statsRepo.GetPickPositionStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pick position stats: %w", err)
	}

	ratingStats, err := h.statsRepo.GetRatingStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get rating stats: %w", err)
	}

	deviationStats, err := h.statsRepo.GetDeviationStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get deviation stats: %w", err)
	}

	selfRatingStats, err := h.statsRepo.GetSelfRatingStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get self rating stats: %w", err)
	}

	pickMetadataStats, err := h.statsRepo.GetPickMetadataStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pick metadata stats: %w", err)
	}

	selfInflationStats, err := h.statsRepo.GetSelfInflationStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get self inflation stats: %w", err)
	}

	lastPickRatingStats, err := h.statsRepo.GetLastPickRatingStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get last pick rating stats: %w", err)
	}

	movieVariance, err := h.statsRepo.GetMovieRatingVariance(ctx)
	if err != nil {
		return nil, fmt.Errorf("get movie variance: %w", err)
	}

	pickCounts, err := h.statsRepo.GetPickCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pick counts: %w", err)
	}

	totalWatched, totalRuntime, totalGroups, fullyRated, err := h.statsRepo.GetSummaryStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get summary stats: %w", err)
	}

	// Build person stats map
	personStatsMap := h.buildPersonStatsMap(
		persons,
		pickPositionStats,
		ratingStats,
		deviationStats,
		selfRatingStats,
		pickMetadataStats,
		selfInflationStats,
		lastPickRatingStats,
		pickCounts,
	)

	// Calculate awards
	awards := h.calculateAwards(personStatsMap, persons)

	// Calculate movie awards
	movieAwards := h.calculateMovieAwards(movieVariance)

	// Build leaderboards
	leaderboards := h.buildLeaderboards(personStatsMap, persons)

	// Convert person stats map to slice
	var personStatsList []model.PersonStats
	for _, ps := range personStatsMap {
		personStatsList = append(personStatsList, ps)
	}

	return &model.StatsData{
		AdvantageHolder:       advantageHolder,
		AdvantageGroup:        advantageGroup,
		Awards:                awards,
		MovieAwards:           movieAwards,
		Leaderboards:          leaderboards,
		PersonStats:           personStatsList,
		TotalMoviesWatched:    totalWatched,
		TotalWatchTimeMinutes: totalRuntime,
		TotalGroups:           totalGroups,
		FullyRatedMovies:      fullyRated,
	}, nil
}

// buildPersonStatsMap combines all stats into PersonStats structs
func (h *StatsHandler) buildPersonStatsMap(
	persons map[uuid.UUID]*model.Person,
	pickPositionStats []model.PickPositionStats,
	ratingStats []model.RatingStats,
	deviationStats []model.DeviationStats,
	selfRatingStats []model.SelfRatingStats,
	pickMetadataStats []model.PickMetadataStats,
	selfInflationStats []model.SelfInflationStats,
	lastPickRatingStats []model.LastPickRatingStats,
	pickCounts map[uuid.UUID]int,
) map[uuid.UUID]model.PersonStats {
	statsMap := make(map[uuid.UUID]model.PersonStats)

	// Initialize with persons
	for id, p := range persons {
		statsMap[id] = model.PersonStats{
			Person:     p,
			TotalPicks: pickCounts[id],
		}
	}

	// Add pick position stats
	for _, pps := range pickPositionStats {
		if ps, ok := statsMap[pps.PersonID]; ok {
			ps.FirstPickCount = pps.FirstPickCount
			ps.LastPickCount = pps.LastPickCount
			statsMap[pps.PersonID] = ps
		}
	}

	// Add rating stats
	for _, rs := range ratingStats {
		if ps, ok := statsMap[rs.PersonID]; ok {
			ps.AvgRatingGiven = rs.AvgRatingGiven
			ps.AvgRatingReceived = rs.AvgRatingReceived
			ps.RatingStdDev = rs.RatingStdDev
			ps.MoviesRated = rs.TotalRatingsGiven
			statsMap[rs.PersonID] = ps
		}
	}

	// Add deviation stats
	for _, ds := range deviationStats {
		if ps, ok := statsMap[ds.PersonID]; ok {
			ps.AvgDeviationFromGroup = ds.AvgDeviation
			statsMap[ds.PersonID] = ps
		}
	}

	// Add self rating stats
	for _, srs := range selfRatingStats {
		if ps, ok := statsMap[srs.PersonID]; ok {
			ps.SelfLowestCount = srs.SelfLowestCount
			statsMap[srs.PersonID] = ps
		}
	}

	// Add pick metadata stats
	for _, pms := range pickMetadataStats {
		if ps, ok := statsMap[pms.PersonID]; ok {
			ps.TotalRuntimePicked = pms.TotalRuntime
			ps.AvgReleaseYear = pms.AvgReleaseYear
			ps.AvgRuntimePerPick = pms.AvgRuntime
			ps.MinReleaseYear = pms.MinReleaseYear
			ps.MaxReleaseYear = pms.MaxReleaseYear
			ps.AvgPickStdDev = pms.AvgPickStdDev
			statsMap[pms.PersonID] = ps
		}
	}

	// Add self inflation stats
	for _, sis := range selfInflationStats {
		if ps, ok := statsMap[sis.PersonID]; ok {
			ps.SelfInflationCount = sis.SelfInflationCount
			statsMap[sis.PersonID] = ps
		}
	}

	// Add last pick rating stats
	for _, lprs := range lastPickRatingStats {
		if ps, ok := statsMap[lprs.PersonID]; ok {
			ps.LastPickAvgRating = lprs.LastPickAvgRating
			statsMap[lprs.PersonID] = ps
		}
	}

	return statsMap
}

// calculateAwards determines who wins each award
func (h *StatsHandler) calculateAwards(statsMap map[uuid.UUID]model.PersonStats, persons map[uuid.UUID]*model.Person) []model.Award {
	var awards []model.Award

	// The Headliner - most first picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return float64(ps.FirstPickCount)
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "headliner",
			Title:       "The Headliner",
			Description: "Always opening night material",
			Icon:        "crown",
			Winner:      winner,
			Value:       fmt.Sprintf("%d first picks", int(value)),
		})
	}

	// The Biggest Loser - most last picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return float64(ps.LastPickCount)
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "biggest_loser",
			Title:       "The Biggest Loser",
			Description: "The comeback kid (3 entries next time!)",
			Icon:        "slot-machine",
			Winner:      winner,
			Value:       fmt.Sprintf("%d last picks", int(value)),
		})
	}

	// Corporate Darling - highest avg rating received on picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 {
			return 0
		}
		return ps.AvgRatingReceived
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "corporate_darling",
			Title:       "Corporate Darling",
			Description: "The family always approves",
			Icon:        "briefcase",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f avg on picks", value),
		})
	}

	// Harsh Critic - lowest avg rating given
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.MoviesRated == 0 {
			return 999
		}
		return ps.AvgRatingGiven
	}); winner != nil && value < 999 {
		awards = append(awards, model.Award{
			ID:          "harsh_critic",
			Title:       "The Harsh Critic",
			Description: "Tough crowd, party of one",
			Icon:        "monocle",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f avg given", value),
		})
	}

	// Easy Pleaser - highest avg rating given
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.MoviesRated == 0 {
			return 0
		}
		return ps.AvgRatingGiven
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "easy_pleaser",
			Title:       "The Easy Pleaser",
			Description: "Everything's a 10 with popcorn",
			Icon:        "smile",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f avg given", value),
		})
	}

	// Critical Outlier - highest avg deviation from group
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return ps.AvgDeviationFromGroup
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "critical_outlier",
			Title:       "The Critical Outlier",
			Description: "Marching to their own projector",
			Icon:        "theater-masks",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f points different on average", value),
		})
	}

	// Movie Masochist - most times rating own pick lowest
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return float64(ps.SelfLowestCount)
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "movie_masochist",
			Title:       "The Movie Masochist",
			Description: "Picks 'em, then roasts 'em",
			Icon:        "sweat-smile",
			Winner:      winner,
			Value:       fmt.Sprintf("%d times", int(value)),
		})
	}

	// The Steady Hand - lowest rating stddev (most consistent)
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.MoviesRated == 0 {
			return 999
		}
		return ps.RatingStdDev
	}); winner != nil && value < 999 {
		awards = append(awards, model.Award{
			ID:          "steady_hand",
			Title:       "The Steady Hand",
			Description: "You always know what you're getting",
			Icon:        "ruler",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f rating spread", value),
		})
	}

	// The Wildcard - highest rating stddev (most inconsistent)
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.MoviesRated == 0 {
			return 0
		}
		return ps.RatingStdDev
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "wildcard",
			Title:       "The Wildcard",
			Description: "10 or 2, no in-between",
			Icon:        "dice",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f rating spread", value),
		})
	}

	// Throwback Royalty - oldest avg release year on picks
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 || ps.AvgReleaseYear == 0 {
			return 9999
		}
		return ps.AvgReleaseYear
	}); winner != nil && value < 9999 {
		awards = append(awards, model.Award{
			ID:          "throwback_royalty",
			Title:       "Throwback Royalty",
			Description: "They don't make 'em like they used to",
			Icon:        "vhs-tape",
			Winner:      winner,
			Value:       fmt.Sprintf("avg year: %.0f", value),
		})
	}

	// Fresh Picker - newest avg release year on picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 {
			return 0
		}
		return ps.AvgReleaseYear
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "fresh_picker",
			Title:       "The Fresh Picker",
			Description: "First in line at the multiplex",
			Icon:        "popcorn",
			Winner:      winner,
			Value:       fmt.Sprintf("avg year: %.0f", value),
		})
	}

	// Marathon Runner - longest total runtime on picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return float64(ps.TotalRuntimePicked)
	}); winner != nil && value > 0 {
		hours := int(value) / 60
		mins := int(value) % 60
		awards = append(awards, model.Award{
			ID:          "marathon_runner",
			Title:       "The Marathon Runner",
			Description: "Bladder of steel",
			Icon:        "stopwatch",
			Winner:      winner,
			Value:       fmt.Sprintf("%dh %dm total", hours, mins),
		})
	}

	// The Grade Inflator - biggest gap between avg rating given vs received
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 || ps.MoviesRated == 0 {
			return 0
		}
		return ps.AvgRatingGiven - ps.AvgRatingReceived
	}); winner != nil && value > 0.5 {
		awards = append(awards, model.Award{
			ID:          "grade_inflator",
			Title:       "The Grade Inflator",
			Description: "Dishes out 9s, gets back 5s",
			Icon:        "balloon",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f point gap", value),
		})
	}

	// The Snooze Button - shortest average runtime per pick
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 {
			return 999
		}
		return ps.AvgRuntimePerPick
	}); winner != nil && value < 999 && value > 0 {
		awards = append(awards, model.Award{
			ID:          "snooze_button",
			Title:       "The Snooze Button",
			Description: "90 minutes or bust",
			Icon:        "alarm-clock",
			Winner:      winner,
			Value:       fmt.Sprintf("%.0f min avg", value),
		})
	}

	// The Binge Enabler - longest average runtime per pick
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 {
			return 0
		}
		return ps.AvgRuntimePerPick
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "binge_enabler",
			Title:       "The Binge Enabler",
			Description: "Every pick is a commitment",
			Icon:        "hourglass",
			Winner:      winner,
			Value:       fmt.Sprintf("%.0f min avg", value),
		})
	}

	// The Time Traveler - biggest spread in release years
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks <= 1 || ps.MinReleaseYear == 0 {
			return 0
		}
		return float64(ps.MaxReleaseYear - ps.MinReleaseYear)
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "time_traveler",
			Title:       "The Time Traveler",
			Description: "From Hitchcock to TikTok",
			Icon:        "clock",
			Winner:      winner,
			Value:       fmt.Sprintf("%d year spread", int(value)),
		})
	}

	// The Hype Machine - person whose picks have highest avg stddev
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 {
			return 0
		}
		return ps.AvgPickStdDev
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "hype_machine",
			Title:       "The Hype Machine",
			Description: "Their picks start arguments",
			Icon:        "megaphone",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f avg stddev", value),
		})
	}

	// The Safe Bet - person whose picks have lowest avg stddev
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks == 0 || ps.AvgPickStdDev == 0 {
			return 999
		}
		return ps.AvgPickStdDev
	}); winner != nil && value < 999 {
		awards = append(awards, model.Award{
			ID:          "safe_bet",
			Title:       "The Safe Bet",
			Description: "Everyone kinda likes them",
			Icon:        "handshake",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f avg stddev", value),
		})
	}

	// The Revisionist - rates own picks higher than group avg most often
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		return float64(ps.SelfInflationCount)
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "revisionist",
			Title:       "The Revisionist",
			Description: "I still stand by it!",
			Icon:        "pen",
			Winner:      winner,
			Value:       fmt.Sprintf("%d times", int(value)),
		})
	}

	// The Validator - ratings closest to group average (opposite of outlier)
	if winner, value := h.findMin(statsMap, func(ps model.PersonStats) float64 {
		if ps.MoviesRated == 0 {
			return 999
		}
		return ps.AvgDeviationFromGroup
	}); winner != nil && value < 999 {
		awards = append(awards, model.Award{
			ID:          "validator",
			Title:       "The Validator",
			Description: "The voice of the people",
			Icon:        "checkmark",
			Winner:      winner,
			Value:       fmt.Sprintf("%.1f points from avg", value),
		})
	}

	// The Century Hopper - picks spanning the most decades
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.TotalPicks <= 1 || ps.MinReleaseYear == 0 {
			return 0
		}
		// Calculate distinct decades spanned (inclusive)
		decades := float64(ps.MaxReleaseYear/10 - ps.MinReleaseYear/10 + 1)
		return decades
	}); winner != nil && value > 0 {
		awards = append(awards, model.Award{
			ID:          "century_hopper",
			Title:       "The Century Hopper",
			Description: "Four generations, one queue",
			Icon:        "calendar",
			Winner:      winner,
			Value:       fmt.Sprintf("%.0f decade(s)", value),
		})
	}

	// The Underdog - most last picks but highest avg rating on those picks
	if winner, value := h.findMax(statsMap, func(ps model.PersonStats) float64 {
		if ps.LastPickCount == 0 || ps.LastPickAvgRating == 0 {
			return 0
		}
		// Weight by number of last picks to reward consistency
		return ps.LastPickAvgRating
	}); winner != nil && value > 0 {
		ps := statsMap[winner.ID]
		if ps.LastPickCount > 0 {
			awards = append(awards, model.Award{
				ID:          "underdog",
				Title:       "The Underdog",
				Description: "The algorithm was wrong about you",
				Icon:        "dog",
				Winner:      winner,
				Value:       fmt.Sprintf("%.1f avg on %d last pick(s)", value, ps.LastPickCount),
			})
		}
	}

	return awards
}

// calculateMovieAwards determines which movies win the movie awards
func (h *StatsHandler) calculateMovieAwards(movieVariance []model.MovieWithStats) []model.MovieAward {
	var awards []model.MovieAward

	if len(movieVariance) == 0 {
		return awards
	}

	// The Hype Train - highest variance (most divisive)
	hypeTrain := movieVariance[0] // already sorted by stddev DESC
	if hypeTrain.RatingStdDev > 0 {
		awards = append(awards, model.MovieAward{
			ID:          "hype_train",
			Title:       "The Hype Train",
			Description: "Love it or hate it",
			Icon:        "train",
			Movie:       hypeTrain.Movie,
			Entry:       hypeTrain.Entry,
			Value:       fmt.Sprintf("Rating spread: %.1f", hypeTrain.RatingStdDev),
		})
	}

	// The Unifier - lowest variance (everyone agreed)
	unifier := movieVariance[len(movieVariance)-1]
	if len(movieVariance) > 1 {
		awards = append(awards, model.MovieAward{
			ID:          "unifier",
			Title:       "The Unifier",
			Description: "Rare family consensus",
			Icon:        "handshake",
			Movie:       unifier.Movie,
			Entry:       unifier.Entry,
			Value:       fmt.Sprintf("Rating spread: %.1f", unifier.RatingStdDev),
		})
	}

	return awards
}

// buildLeaderboards creates the leaderboard data
func (h *StatsHandler) buildLeaderboards(statsMap map[uuid.UUID]model.PersonStats, persons map[uuid.UUID]*model.Person) []model.Leaderboard {
	var leaderboards []model.Leaderboard

	// Generosity Index (avg rating given)
	var generosityEntries []model.LeaderboardEntry
	var maxGenerosity float64
	for _, ps := range statsMap {
		if ps.MoviesRated > 0 {
			generosityEntries = append(generosityEntries, model.LeaderboardEntry{
				Person: ps.Person,
				Value:  ps.AvgRatingGiven,
				Label:  fmt.Sprintf("%.1f", ps.AvgRatingGiven),
			})
			if ps.AvgRatingGiven > maxGenerosity {
				maxGenerosity = ps.AvgRatingGiven
			}
		}
	}
	sort.Slice(generosityEntries, func(i, j int) bool {
		return generosityEntries[i].Value > generosityEntries[j].Value
	})
	if len(generosityEntries) > 0 {
		leaderboards = append(leaderboards, model.Leaderboard{
			Title:    "Generosity Index",
			Icon:     "gift",
			Entries:  generosityEntries,
			MaxValue: maxGenerosity,
		})
	}

	// Pick Success Rate (avg rating received on picks)
	var successEntries []model.LeaderboardEntry
	var maxSuccess float64
	for _, ps := range statsMap {
		if ps.TotalPicks > 0 && ps.AvgRatingReceived > 0 {
			successEntries = append(successEntries, model.LeaderboardEntry{
				Person: ps.Person,
				Value:  ps.AvgRatingReceived,
				Label:  fmt.Sprintf("%.1f", ps.AvgRatingReceived),
			})
			if ps.AvgRatingReceived > maxSuccess {
				maxSuccess = ps.AvgRatingReceived
			}
		}
	}
	sort.Slice(successEntries, func(i, j int) bool {
		return successEntries[i].Value > successEntries[j].Value
	})
	if len(successEntries) > 0 {
		leaderboards = append(leaderboards, model.Leaderboard{
			Title:    "Pick Success Rate",
			Icon:     "target",
			Entries:  successEntries,
			MaxValue: maxSuccess,
		})
	}

	// Movies Picked
	var pickEntries []model.LeaderboardEntry
	var maxPicks float64
	for _, ps := range statsMap {
		if ps.TotalPicks > 0 {
			pickEntries = append(pickEntries, model.LeaderboardEntry{
				Person: ps.Person,
				Value:  float64(ps.TotalPicks),
				Label:  fmt.Sprintf("%d", ps.TotalPicks),
			})
			if float64(ps.TotalPicks) > maxPicks {
				maxPicks = float64(ps.TotalPicks)
			}
		}
	}
	sort.Slice(pickEntries, func(i, j int) bool {
		return pickEntries[i].Value > pickEntries[j].Value
	})
	if len(pickEntries) > 0 {
		leaderboards = append(leaderboards, model.Leaderboard{
			Title:    "Total Picks",
			Icon:     "clapperboard",
			Entries:  pickEntries,
			MaxValue: maxPicks,
		})
	}

	return leaderboards
}

// findMax finds the person with the maximum value for the given metric
func (h *StatsHandler) findMax(statsMap map[uuid.UUID]model.PersonStats, metric func(model.PersonStats) float64) (*model.Person, float64) {
	var winner *model.Person
	var maxVal float64 = -1

	for _, ps := range statsMap {
		val := metric(ps)
		if val > maxVal {
			maxVal = val
			winner = ps.Person
		}
	}

	return winner, maxVal
}

// findMin finds the person with the minimum value for the given metric
func (h *StatsHandler) findMin(statsMap map[uuid.UUID]model.PersonStats, metric func(model.PersonStats) float64) (*model.Person, float64) {
	var winner *model.Person
	minVal := math.Inf(1)

	for _, ps := range statsMap {
		val := metric(ps)
		if val < minVal {
			minVal = val
			winner = ps.Person
		}
	}

	return winner, minVal
}
