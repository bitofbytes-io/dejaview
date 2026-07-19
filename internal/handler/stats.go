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
	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/ui/pages"
)

const topMovieLimit = 5

// StatsHandler handles the Trophy Room.
type StatsHandler struct {
	statsRepo      *repository.StatsRepository
	sessionManager *session.Manager
}

// NewStatsHandler creates a new StatsHandler.
func NewStatsHandler(statsRepo *repository.StatsRepository, sessionManager *session.Manager) *StatsHandler {
	return &StatsHandler{statsRepo: statsRepo, sessionManager: sessionManager}
}

// StatsPage renders the Trophy Room.
func (h *StatsHandler) StatsPage(w http.ResponseWriter, r *http.Request) {
	statsData, err := h.buildStatsData(r.Context())
	if err != nil {
		slog.Error("failed to build trophy room data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := isAuthenticatedRequest(r, h.sessionManager)
	if err := pages.StatsPage(statsData, isAuthenticated).Render(r.Context(), w); err != nil {
		slog.Error("failed to render trophy room", "error", err)
	}
}

func (h *StatsHandler) buildStatsData(ctx context.Context) (*model.StatsData, error) {
	requiredRatings := len(model.FamilyInitials)

	currentGroup, err := h.statsRepo.GetCurrentGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current group: %w", err)
	}

	advantageHolder, advantageGroup, err := h.statsRepo.GetAdvantageHolder(ctx, currentGroup)
	if err != nil {
		return nil, fmt.Errorf("get advantage holder: %w", err)
	}

	trophyStats, err := h.statsRepo.GetTrophyStats(ctx, requiredRatings)
	if err != nil {
		return nil, fmt.Errorf("get trophy stats: %w", err)
	}

	topMovies, err := h.statsRepo.GetTopRatedMovies(ctx, requiredRatings, topMovieLimit)
	if err != nil {
		return nil, fmt.Errorf("get top rated movies: %w", err)
	}

	totalWatched, totalRuntime, fullyRated, err := h.statsRepo.GetSummaryStats(ctx, requiredRatings)
	if err != nil {
		return nil, fmt.Errorf("get summary stats: %w", err)
	}

	return &model.StatsData{
		AdvantageHolder:       advantageHolder,
		AdvantageGroup:        advantageGroup,
		Trophies:              calculateTrophies(trophyStats),
		TopMovies:             topMovies,
		TotalMoviesWatched:    totalWatched,
		TotalWatchTimeMinutes: totalRuntime,
		FullyRatedMovies:      fullyRated,
	}, nil
}

func calculateTrophies(stats []model.TrophyStats) []model.Trophy {
	crowdWinners, crowdValue := maxWinners(stats,
		func(stat model.TrophyStats) bool { return stat.FullyRatedPicks > 0 },
		func(stat model.TrophyStats) float64 { return stat.AvgRatingReceived },
	)
	fanWinners, fanValue := maxWinners(stats,
		func(stat model.TrophyStats) bool { return stat.RatingsGiven > 0 },
		func(stat model.TrophyStats) float64 { return stat.AvgRatingGiven },
	)
	marathonWinners, marathonValue := maxWinners(stats,
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

func maxWinners(
	stats []model.TrophyStats,
	eligible func(model.TrophyStats) bool,
	metric func(model.TrophyStats) float64,
) ([]*model.Person, float64) {
	best := math.Inf(-1)
	var winners []*model.Person

	for _, stat := range stats {
		if stat.Person == nil || !eligible(stat) {
			continue
		}
		value := metric(stat)
		switch {
		case value > best && !nearlyEqual(value, best):
			best = value
			winners = []*model.Person{stat.Person}
		case nearlyEqual(value, best):
			winners = append(winners, stat.Person)
		}
	}

	sort.Slice(winners, func(i, j int) bool { return winners[i].Name < winners[j].Name })
	if len(winners) == 0 {
		return nil, 0
	}
	return winners, best
}

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
	return fmt.Sprintf("%s of picks", formatRuntime(minutes))
}

func formatRuntime(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	hours := minutes / 60
	mins := minutes % 60
	if hours > 0 && mins > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", mins)
}
