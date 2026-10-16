package repository

import (
	"context"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StatsRepository handles database operations for the Trophy Room.
type StatsRepository struct {
	pool *pgxpool.Pool
}

// NewStatsRepository creates a new StatsRepository.
func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

// GetAdvantageHolder returns the person who picked last in the group before
// the current (highest) one, and that group's number. The previous group is
// the highest group below the current one that has entries, so an emptied
// group number is skipped. Before a second group exists it returns nil and 0;
// a previous group with no picker for its last entry returns nil and the
// group number.
func (r *StatsRepository) GetAdvantageHolder(ctx context.Context) (*model.Person, int, error) {
	query := `
		WITH previous AS (
			SELECT MAX(group_number) AS group_number
			FROM entries
			WHERE group_number < (SELECT MAX(group_number) FROM entries)
		)
		SELECT previous.group_number, p.id, p.initial, p.name, p.color
		FROM previous
		LEFT JOIN LATERAL (
			SELECT e.picked_by_person_id
			FROM entries e
			WHERE e.group_number = previous.group_number
			ORDER BY e.position DESC
			LIMIT 1
		) last_pick ON true
		LEFT JOIN persons p ON p.id = last_pick.picked_by_person_id`

	var prevGroup *int
	var personID *uuid.UUID
	var initial, name, color *string
	if err := r.pool.QueryRow(ctx, query).Scan(&prevGroup, &personID, &initial, &name, &color); err != nil {
		return nil, 0, fmt.Errorf("get advantage holder: %w", err)
	}
	if prevGroup == nil || *prevGroup < 1 {
		return nil, 0, nil
	}
	if personID == nil || initial == nil || name == nil || color == nil {
		return nil, *prevGroup, nil
	}
	return &model.Person{ID: *personID, Initial: *initial, Name: *name, Color: *color}, *prevGroup, nil
}

// fullyRated is the HAVING clause, over ratings grouped by entry_id, that
// keeps the entries that count toward the Trophy Room: those rated by all
// but one person, and by at least one. It matches
// model.TrophyRatingThreshold.
const fullyRated = `HAVING COUNT(DISTINCT person_id) >= GREATEST((SELECT COUNT(*) FROM persons) - 1, 1)`

// GetTrophyStats returns the three straightforward metrics used by the Trophy Room.
func (r *StatsRepository) GetTrophyStats(ctx context.Context) ([]model.TrophyStats, error) {
	query := `
		WITH entry_scores AS (
			SELECT entry_id, AVG(score) AS average_rating
			FROM ratings
			GROUP BY entry_id
			` + fullyRated + `
		),
		ratings_given AS (
			SELECT r.person_id, AVG(r.score) AS average, COUNT(*) AS rating_count
			FROM ratings r
			JOIN entry_scores es ON es.entry_id = r.entry_id
			GROUP BY r.person_id
		),
		-- Average each pick's family score, so a pick rated by three people
		-- weighs the same as one rated by four.
		ratings_received AS (
			SELECT e.picked_by_person_id AS person_id,
				AVG(es.average_rating) AS average,
				COUNT(*) AS pick_count
			FROM entries e
			JOIN entry_scores es ON es.entry_id = e.id
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		),
		picked_runtime AS (
			SELECT e.picked_by_person_id AS person_id,
				COALESCE(SUM(m.runtime_minutes), 0) AS total_runtime,
				COUNT(m.runtime_minutes) AS runtime_pick_count
			FROM entries e
			JOIN movies m ON m.id = e.movie_id
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		)
		SELECT p.id, p.initial, p.name, p.color,
			COALESCE(rg.average, 0), COALESCE(rg.rating_count, 0),
			COALESCE(rr.average, 0), COALESCE(rr.pick_count, 0),
			COALESCE(pr.total_runtime, 0), COALESCE(pr.runtime_pick_count, 0)
		FROM persons p
		LEFT JOIN ratings_given rg ON rg.person_id = p.id
		LEFT JOIN ratings_received rr ON rr.person_id = p.id
		LEFT JOIN picked_runtime pr ON pr.person_id = p.id
		ORDER BY p.name`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get trophy stats: %w", err)
	}
	defer rows.Close()

	var stats []model.TrophyStats
	for rows.Next() {
		person := &model.Person{}
		var stat model.TrophyStats
		if err := rows.Scan(
			&person.ID,
			&person.Initial,
			&person.Name,
			&person.Color,
			&stat.AvgRatingGiven,
			&stat.RatingsGiven,
			&stat.AvgRatingReceived,
			&stat.FullyRatedPicks,
			&stat.TotalRuntimePicked,
			&stat.PicksWithKnownRuntime,
		); err != nil {
			return nil, fmt.Errorf("scan trophy stats: %w", err)
		}
		stat.Person = person
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// GetTopRatedMovies returns up to limit family-rated entries ranked by family average.
func (r *StatsRepository) GetTopRatedMovies(ctx context.Context, limit int) ([]model.RankedMovie, error) {
	query := `
		WITH entry_scores AS (
			SELECT entry_id, AVG(score) AS average_rating
			FROM ratings
			GROUP BY entry_id
			` + fullyRated + `
		)
		SELECT e.id, e.movie_id, e.group_number, e.position, e.added_at, e.picked_by_person_id,
			m.id, m.title, m.release_year, m.poster_url, m.runtime_minutes,
			p.id, p.initial, p.name, p.color,
			es.average_rating
		FROM entry_scores es
		JOIN entries e ON e.id = es.entry_id
		JOIN movies m ON m.id = e.movie_id
		LEFT JOIN persons p ON p.id = e.picked_by_person_id
		ORDER BY es.average_rating DESC, LOWER(m.title), e.id
		LIMIT $1`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get top rated movies: %w", err)
	}
	defer rows.Close()

	var movies []model.RankedMovie
	for rows.Next() {
		entry := &model.Entry{}
		movie := &model.Movie{}
		var pickerID *uuid.UUID
		var pickerInitial, pickerName, pickerColor *string
		var ranked model.RankedMovie

		if err := rows.Scan(
			&entry.ID,
			&entry.MovieID,
			&entry.GroupNumber,
			&entry.Position,
			&entry.AddedAt,
			&entry.PickedByPersonID,
			&movie.ID,
			&movie.Title,
			&movie.ReleaseYear,
			&movie.PosterURL,
			&movie.RuntimeMinutes,
			&pickerID,
			&pickerInitial,
			&pickerName,
			&pickerColor,
			&ranked.AverageRating,
		); err != nil {
			return nil, fmt.Errorf("scan top rated movie: %w", err)
		}

		entry.Movie = movie
		if pickerID != nil && pickerInitial != nil && pickerName != nil && pickerColor != nil {
			entry.PickedByPerson = &model.Person{ID: *pickerID, Initial: *pickerInitial, Name: *pickerName, Color: *pickerColor}
		}
		ranked.Entry = entry
		movies = append(movies, ranked)
	}

	return movies, rows.Err()
}

// GetGenrePickStats returns, for each picker and genre in the saved TMDB
// metadata, how many picks carry the genre and how the family rated them.
// Movies without genre metadata are skipped, and only family-rated entries
// (see fullyRated) count toward ratings.
func (r *StatsRepository) GetGenrePickStats(ctx context.Context) ([]model.GenrePickStat, error) {
	query := `
		WITH entry_scores AS (
			SELECT entry_id, AVG(score) AS average_rating
			FROM ratings
			GROUP BY entry_id
			` + fullyRated + `
		),
		entry_genres AS (
			SELECT e.id AS entry_id, e.picked_by_person_id AS person_id, g.genre, es.average_rating
			FROM entries e
			JOIN movies m ON m.id = e.movie_id
			CROSS JOIN LATERAL (
				SELECT DISTINCT BTRIM(elem.value ->> 'name') AS genre
				FROM jsonb_array_elements(
					CASE WHEN jsonb_typeof(m.metadata_json -> 'genres') = 'array'
						THEN m.metadata_json -> 'genres' ELSE '[]'::jsonb END
				) AS elem(value)
				WHERE jsonb_typeof(elem.value) = 'object'
					AND jsonb_typeof(elem.value -> 'name') = 'string'
					AND BTRIM(elem.value ->> 'name') <> ''
			) g
			LEFT JOIN entry_scores es ON es.entry_id = e.id
		),
		tagged AS (
			SELECT person_id, COUNT(DISTINCT entry_id) AS tagged_picks
			FROM entry_genres
			GROUP BY person_id
		)
		SELECT p.id, p.initial, p.name, p.color, eg.genre,
			COUNT(*), MAX(t.tagged_picks),
			COUNT(eg.average_rating), COALESCE(SUM(eg.average_rating), 0)
		FROM entry_genres eg
		JOIN tagged t ON t.person_id IS NOT DISTINCT FROM eg.person_id
		LEFT JOIN persons p ON p.id = eg.person_id
		GROUP BY p.id, p.initial, p.name, p.color, eg.genre
		ORDER BY p.name, eg.genre`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get genre pick stats: %w", err)
	}
	defer rows.Close()

	var stats []model.GenrePickStat
	for rows.Next() {
		var personID *uuid.UUID
		var initial, name, color *string
		var stat model.GenrePickStat
		if err := rows.Scan(
			&personID, &initial, &name, &color, &stat.Genre,
			&stat.Picks, &stat.TaggedPicks, &stat.RatedPicks, &stat.RatingTotal,
		); err != nil {
			return nil, fmt.Errorf("scan genre pick stat: %w", err)
		}
		if personID != nil && initial != nil && name != nil && color != nil {
			stat.Person = &model.Person{ID: *personID, Initial: *initial, Name: *name, Color: *color}
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// GetSummaryStats returns the compact family totals displayed at the bottom of the page.
func (r *StatsRepository) GetSummaryStats(ctx context.Context) (totalWatched, totalRuntime, fullyRatedCount int, err error) {
	query := `
		SELECT
			(SELECT COUNT(*) FROM entries),
			(SELECT COALESCE(SUM(m.runtime_minutes), 0)
			 FROM entries e JOIN movies m ON m.id = e.movie_id),
			(SELECT COUNT(*) FROM (
				SELECT entry_id
				FROM ratings
				GROUP BY entry_id
				` + fullyRated + `
			) fully_rated)`

	err = r.pool.QueryRow(ctx, query).Scan(&totalWatched, &totalRuntime, &fullyRatedCount)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("get summary stats: %w", err)
	}
	return totalWatched, totalRuntime, fullyRatedCount, nil
}
