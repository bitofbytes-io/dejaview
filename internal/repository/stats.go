package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StatsRepository handles database operations for statistics
type StatsRepository struct {
	pool *pgxpool.Pool
}

// NewStatsRepository creates a new StatsRepository
func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

// GetAdvantageHolder returns the person who picked last in the previous group
// (they get the 3-pick advantage for the next draw)
func (r *StatsRepository) GetAdvantageHolder(ctx context.Context, currentGroup int) (*model.Person, int, error) {
	if currentGroup <= 1 {
		return nil, 0, nil // No advantage holder for first group
	}

	prevGroup := currentGroup - 1

	query := `
		WITH group_max AS (
			SELECT MAX(position) as max_pos
			FROM entries
			WHERE group_number = $1
		)
		SELECT p.id, p.initial, p.name
		FROM entries e
		JOIN persons p ON e.picked_by_person_id = p.id
		JOIN group_max gm ON e.position = gm.max_pos
		WHERE e.group_number = $1
		LIMIT 1`

	person := &model.Person{}
	err := r.pool.QueryRow(ctx, query, prevGroup).Scan(
		&person.ID,
		&person.Initial,
		&person.Name,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No rows is fine - might be no picker assigned
			return nil, prevGroup, nil
		}
		return nil, prevGroup, fmt.Errorf("get advantage holder: %w", err)
	}

	return person, prevGroup, nil
}

// GetPickPositionStats returns first/last pick counts per person
func (r *StatsRepository) GetPickPositionStats(ctx context.Context) ([]model.PickPositionStats, error) {
	query := `
		WITH group_bounds AS (
			SELECT 
				group_number,
				MIN(position) as min_pos,
				MAX(position) as max_pos
			FROM entries
			GROUP BY group_number
		),
		first_picks AS (
			SELECT e.picked_by_person_id as person_id, COUNT(*) as cnt
			FROM entries e
			JOIN group_bounds gb ON e.group_number = gb.group_number AND e.position = gb.min_pos
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		),
		last_picks AS (
			SELECT e.picked_by_person_id as person_id, COUNT(*) as cnt
			FROM entries e
			JOIN group_bounds gb ON e.group_number = gb.group_number AND e.position = gb.max_pos
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		)
		SELECT 
			p.id,
			COALESCE(fp.cnt, 0) as first_pick_count,
			COALESCE(lp.cnt, 0) as last_pick_count
		FROM persons p
		LEFT JOIN first_picks fp ON p.id = fp.person_id
		LEFT JOIN last_picks lp ON p.id = lp.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pick position stats: %w", err)
	}
	defer rows.Close()

	var stats []model.PickPositionStats
	for rows.Next() {
		var s model.PickPositionStats
		if err := rows.Scan(&s.PersonID, &s.FirstPickCount, &s.LastPickCount); err != nil {
			return nil, fmt.Errorf("scan pick position stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetRatingStats returns rating statistics per person
// Only considers entries with all 4 ratings (fully rated)
func (r *StatsRepository) GetRatingStats(ctx context.Context) ([]model.RatingStats, error) {
	query := `
		WITH fully_rated_entries AS (
			SELECT entry_id
			FROM ratings
			GROUP BY entry_id
			HAVING COUNT(*) = 4
		),
		rating_given AS (
			SELECT 
				r.person_id,
				AVG(r.score) as avg_given,
				STDDEV_POP(r.score) as stddev_given,
				COUNT(*) as total_given
			FROM ratings r
			JOIN fully_rated_entries fre ON r.entry_id = fre.entry_id
			GROUP BY r.person_id
		),
		rating_received AS (
			SELECT 
				e.picked_by_person_id as person_id,
				AVG(r.score) as avg_received
			FROM entries e
			JOIN ratings r ON e.id = r.entry_id
			JOIN fully_rated_entries fre ON e.id = fre.entry_id
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		)
		SELECT 
			p.id,
			COALESCE(rg.avg_given, 0) as avg_rating_given,
			COALESCE(rr.avg_received, 0) as avg_rating_received,
			COALESCE(rg.stddev_given, 0) as rating_stddev,
			COALESCE(rg.total_given, 0) as total_ratings_given
		FROM persons p
		LEFT JOIN rating_given rg ON p.id = rg.person_id
		LEFT JOIN rating_received rr ON p.id = rr.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get rating stats: %w", err)
	}
	defer rows.Close()

	var stats []model.RatingStats
	for rows.Next() {
		var s model.RatingStats
		if err := rows.Scan(
			&s.PersonID,
			&s.AvgRatingGiven,
			&s.AvgRatingReceived,
			&s.RatingStdDev,
			&s.TotalRatingsGiven,
		); err != nil {
			return nil, fmt.Errorf("scan rating stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetDeviationStats returns how much each person's ratings deviate from group average
func (r *StatsRepository) GetDeviationStats(ctx context.Context) ([]model.DeviationStats, error) {
	query := `
		WITH fully_rated_entries AS (
			SELECT entry_id
			FROM ratings
			GROUP BY entry_id
			HAVING COUNT(*) = 4
		),
		entry_averages AS (
			SELECT entry_id, AVG(score) as avg_score
			FROM ratings
			WHERE entry_id IN (SELECT entry_id FROM fully_rated_entries)
			GROUP BY entry_id
		),
		deviations AS (
			SELECT 
				r.person_id,
				AVG(ABS(r.score - ea.avg_score)) as avg_deviation
			FROM ratings r
			JOIN entry_averages ea ON r.entry_id = ea.entry_id
			GROUP BY r.person_id
		)
		SELECT p.id, COALESCE(d.avg_deviation, 0)
		FROM persons p
		LEFT JOIN deviations d ON p.id = d.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get deviation stats: %w", err)
	}
	defer rows.Close()

	var stats []model.DeviationStats
	for rows.Next() {
		var s model.DeviationStats
		if err := rows.Scan(&s.PersonID, &s.AvgDeviation); err != nil {
			return nil, fmt.Errorf("scan deviation stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetSelfRatingStats returns how often each person rated their own pick the lowest
func (r *StatsRepository) GetSelfRatingStats(ctx context.Context) ([]model.SelfRatingStats, error) {
	query := `
		WITH fully_rated_entries AS (
			SELECT entry_id
			FROM ratings
			GROUP BY entry_id
			HAVING COUNT(*) = 4
		),
		entry_min_ratings AS (
			SELECT entry_id, MIN(score) as min_score
			FROM ratings
			WHERE entry_id IN (SELECT entry_id FROM fully_rated_entries)
			GROUP BY entry_id
		),
		self_lowest AS (
			SELECT 
				e.picked_by_person_id as person_id,
				COUNT(*) as cnt
			FROM entries e
			JOIN ratings r ON e.id = r.entry_id AND e.picked_by_person_id = r.person_id
			JOIN entry_min_ratings emr ON e.id = emr.entry_id AND r.score = emr.min_score
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id
		)
		SELECT p.id, COALESCE(sl.cnt, 0)
		FROM persons p
		LEFT JOIN self_lowest sl ON p.id = sl.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get self rating stats: %w", err)
	}
	defer rows.Close()

	var stats []model.SelfRatingStats
	for rows.Next() {
		var s model.SelfRatingStats
		if err := rows.Scan(&s.PersonID, &s.SelfLowestCount); err != nil {
			return nil, fmt.Errorf("scan self rating stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetPickMetadataStats returns runtime and release year stats per person
func (r *StatsRepository) GetPickMetadataStats(ctx context.Context) ([]model.PickMetadataStats, error) {
	query := `
		WITH pick_variance AS (
			SELECT 
				e.picked_by_person_id,
				e.id as entry_id,
				STDDEV_POP(r.score) as variance
			FROM entries e
			JOIN ratings r ON e.id = r.entry_id
			WHERE e.picked_by_person_id IS NOT NULL
			GROUP BY e.picked_by_person_id, e.id
			HAVING COUNT(r.score) = 4
		)
		SELECT 
			e.picked_by_person_id,
			COALESCE(SUM(m.runtime_minutes), 0) as total_runtime,
			COALESCE(AVG(m.release_year), 0) as avg_release_year,
			COALESCE(AVG(m.runtime_minutes), 0) as avg_runtime,
			COALESCE(MIN(m.release_year), 0) as min_release_year,
			COALESCE(MAX(m.release_year), 0) as max_release_year,
			COUNT(*) as pick_count,
			COALESCE(AVG(pv.variance), 0) as avg_pick_variance
		FROM entries e
		JOIN movies m ON e.movie_id = m.id
		LEFT JOIN pick_variance pv ON e.id = pv.entry_id
		WHERE e.picked_by_person_id IS NOT NULL
		GROUP BY e.picked_by_person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pick metadata stats: %w", err)
	}
	defer rows.Close()

	var stats []model.PickMetadataStats
	for rows.Next() {
		var s model.PickMetadataStats
		if err := rows.Scan(
			&s.PersonID,
			&s.TotalRuntime,
			&s.AvgReleaseYear,
			&s.AvgRuntime,
			&s.MinReleaseYear,
			&s.MaxReleaseYear,
			&s.PickCount,
			&s.AvgPickVariance,
		); err != nil {
			return nil, fmt.Errorf("scan pick metadata stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetMovieRatingVariance returns movies sorted by rating variance (for Hype Train / Unifier)
func (r *StatsRepository) GetMovieRatingVariance(ctx context.Context) ([]model.MovieWithStats, error) {
	query := `
		WITH fully_rated_entries AS (
			SELECT entry_id
			FROM ratings
			GROUP BY entry_id
			HAVING COUNT(*) = 4
		),
		entry_stats AS (
			SELECT 
				r.entry_id,
				AVG(r.score) as avg_rating,
				STDDEV_POP(r.score) as stddev_rating
			FROM ratings r
			JOIN fully_rated_entries fre ON r.entry_id = fre.entry_id
			GROUP BY r.entry_id
		)
		SELECT e.id, e.movie_id, e.group_number, e.position, e.added_at, e.picked_by_person_id,

			m.id, m.title, m.release_year, m.poster_url, m.runtime_minutes,
			p.id, p.initial, p.name,
			es.avg_rating,
			es.stddev_rating
		FROM entry_stats es
		JOIN entries e ON es.entry_id = e.id
		JOIN movies m ON e.movie_id = m.id
		LEFT JOIN persons p ON e.picked_by_person_id = p.id
		ORDER BY es.stddev_rating DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get movie rating variance: %w", err)
	}
	defer rows.Close()

	var movies []model.MovieWithStats
	for rows.Next() {
		var mws model.MovieWithStats
		entry := &model.Entry{}
		movie := &model.Movie{}
		var pickerID *uuid.UUID
		var pickerInitial, pickerName *string

		if err := rows.Scan(
			&entry.ID, &entry.MovieID, &entry.GroupNumber, &entry.Position,
			&entry.AddedAt, &entry.PickedByPersonID,

			&movie.ID, &movie.Title, &movie.ReleaseYear, &movie.PosterURL, &movie.RuntimeMinutes,
			&pickerID, &pickerInitial, &pickerName,
			&mws.AvgRating, &mws.RatingStdDev,
		); err != nil {
			return nil, fmt.Errorf("scan movie rating variance: %w", err)
		}

		entry.Movie = movie
		mws.Entry = entry
		mws.Movie = movie

		if pickerID != nil && pickerInitial != nil && pickerName != nil {
			mws.Picker = &model.Person{
				ID:      *pickerID,
				Initial: *pickerInitial,
				Name:    *pickerName,
			}
		}

		movies = append(movies, mws)
	}

	return movies, rows.Err()
}

// GetSummaryStats returns overall summary statistics
func (r *StatsRepository) GetSummaryStats(ctx context.Context) (totalWatched, totalRuntime, totalGroups, fullyRated int, err error) {
	query := `
		WITH stats AS (
			SELECT 
				(SELECT COUNT(*) FROM entries) as total_watched,
				(SELECT COALESCE(SUM(m.runtime_minutes), 0) 
				 FROM entries e JOIN movies m ON e.movie_id = m.id) as total_runtime,
				(SELECT COALESCE(MAX(group_number), 0) FROM entries) as total_groups
		),
		fully_rated_count AS (
			SELECT COUNT(*) as cnt FROM (
				SELECT entry_id FROM ratings GROUP BY entry_id HAVING COUNT(*) = 4
			) sub
		)
		SELECT s.total_watched, s.total_runtime, s.total_groups, frc.cnt
		FROM stats s, fully_rated_count frc`

	err = r.pool.QueryRow(ctx, query).Scan(&totalWatched, &totalRuntime, &totalGroups, &fullyRated)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("get summary stats: %w", err)
	}

	return totalWatched, totalRuntime, totalGroups, fullyRated, nil
}

// GetAllPersons returns all persons for lookup
func (r *StatsRepository) GetAllPersons(ctx context.Context) (map[uuid.UUID]*model.Person, error) {
	query := `SELECT id, initial, name FROM persons`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all persons: %w", err)
	}
	defer rows.Close()

	persons := make(map[uuid.UUID]*model.Person)
	for rows.Next() {
		p := &model.Person{}
		if err := rows.Scan(&p.ID, &p.Initial, &p.Name); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		persons[p.ID] = p
	}

	return persons, rows.Err()
}

// GetCurrentGroup returns the current (highest) group number
func (r *StatsRepository) GetCurrentGroup(ctx context.Context) (int, error) {
	query := `SELECT COALESCE(MAX(group_number), 1) FROM entries`

	var group int
	err := r.pool.QueryRow(ctx, query).Scan(&group)
	if err != nil {
		return 1, fmt.Errorf("get current group: %w", err)
	}

	return group, nil
}

// GetSelfInflationStats returns how often each person rated their own pick above group average
func (r *StatsRepository) GetSelfInflationStats(ctx context.Context) ([]model.SelfInflationStats, error) {
	query := `
		WITH fully_rated_entries AS (
			SELECT entry_id
			FROM ratings
			GROUP BY entry_id
			HAVING COUNT(*) = 4
		),
		entry_averages AS (
			SELECT entry_id, AVG(score) as avg_score
			FROM ratings
			WHERE entry_id IN (SELECT entry_id FROM fully_rated_entries)
			GROUP BY entry_id
		),
		self_inflation AS (
			SELECT 
				e.picked_by_person_id as person_id,
				COUNT(*) as cnt
			FROM entries e
			JOIN ratings r ON e.id = r.entry_id AND e.picked_by_person_id = r.person_id
			JOIN entry_averages ea ON e.id = ea.entry_id
			WHERE e.picked_by_person_id IS NOT NULL AND r.score > ea.avg_score
			GROUP BY e.picked_by_person_id
		)
		SELECT p.id, COALESCE(si.cnt, 0)
		FROM persons p
		LEFT JOIN self_inflation si ON p.id = si.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get self inflation stats: %w", err)
	}
	defer rows.Close()

	var stats []model.SelfInflationStats
	for rows.Next() {
		var s model.SelfInflationStats
		if err := rows.Scan(&s.PersonID, &s.SelfInflationCount); err != nil {
			return nil, fmt.Errorf("scan self inflation stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetLastPickRatingStats returns average rating received on last picks per person
func (r *StatsRepository) GetLastPickRatingStats(ctx context.Context) ([]model.LastPickRatingStats, error) {
	query := `
		WITH group_bounds AS (
			SELECT 
				group_number,
				MAX(position) as max_pos
			FROM entries
			GROUP BY group_number
		),
		last_picks AS (
			SELECT e.id as entry_id, e.picked_by_person_id
			FROM entries e
			JOIN group_bounds gb ON e.group_number = gb.group_number AND e.position = gb.max_pos
			WHERE e.picked_by_person_id IS NOT NULL
		),
		fully_rated_last_picks AS (
			SELECT lp.entry_id, lp.picked_by_person_id
			FROM last_picks lp
			WHERE (SELECT COUNT(*) FROM ratings WHERE entry_id = lp.entry_id) = 4
		),
		last_pick_ratings AS (
			SELECT 
				frlp.picked_by_person_id as person_id,
				AVG(r.score) as avg_rating
			FROM fully_rated_last_picks frlp
			JOIN ratings r ON frlp.entry_id = r.entry_id
			GROUP BY frlp.picked_by_person_id
		)
		SELECT p.id, COALESCE(lpr.avg_rating, 0)
		FROM persons p
		LEFT JOIN last_pick_ratings lpr ON p.id = lpr.person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get last pick rating stats: %w", err)
	}
	defer rows.Close()

	var stats []model.LastPickRatingStats
	for rows.Next() {
		var s model.LastPickRatingStats
		if err := rows.Scan(&s.PersonID, &s.LastPickAvgRating); err != nil {
			return nil, fmt.Errorf("scan last pick rating stats: %w", err)
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}

// GetPickCounts returns total picks per person
func (r *StatsRepository) GetPickCounts(ctx context.Context) (map[uuid.UUID]int, error) {
	query := `
		SELECT picked_by_person_id, COUNT(*)
		FROM entries
		WHERE picked_by_person_id IS NOT NULL
		GROUP BY picked_by_person_id`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pick counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var personID uuid.UUID
		var count int
		if err := rows.Scan(&personID, &count); err != nil {
			return nil, fmt.Errorf("scan pick count: %w", err)
		}
		counts[personID] = count
	}

	return counts, rows.Err()
}
