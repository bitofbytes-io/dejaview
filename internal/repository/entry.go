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

// EntryRepository handles database operations for entries
type EntryRepository struct {
	pool *pgxpool.Pool
}

// NewEntryRepository creates a new EntryRepository
func NewEntryRepository(pool *pgxpool.Pool) *EntryRepository {
	return &EntryRepository{pool: pool}
}

func applyPickedByPerson(entry *model.Entry, pickedByPersonDBID *uuid.UUID, pickedByInitial, pickedByName *string) {
	if pickedByPersonDBID != nil && pickedByInitial != nil && pickedByName != nil {
		entry.PickedByPerson = &model.Person{
			ID:      *pickedByPersonDBID,
			Initial: *pickedByInitial,
			Name:    *pickedByName,
		}
	}
}

// Create inserts a new entry into the database
func (r *EntryRepository) Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("create entry begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Serialize position assignment per group to avoid duplicate positions under concurrency.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, $1)", input.GroupNumber); err != nil {
		return nil, fmt.Errorf("create entry lock group: %w", err)
	}

	// Insert with position = max position in group + 1 (or 1 if no entries in group)
	query := `
		INSERT INTO entries (movie_id, group_number, picked_by_person_id, position)
		VALUES ($1, $2, $3, COALESCE((SELECT MAX(position) FROM entries WHERE group_number = $2), 0) + 1)
		RETURNING id, movie_id, group_number, position, added_at, picked_by_person_id`

	entry := &model.Entry{}
	err = tx.QueryRow(ctx, query,
		input.MovieID,
		input.GroupNumber,
		input.PickedByPersonID,
	).Scan(
		&entry.ID,
		&entry.MovieID,
		&entry.GroupNumber,
		&entry.Position,
		&entry.AddedAt,
		&entry.PickedByPersonID,
	)
	if err != nil {
		return nil, fmt.Errorf("create entry: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create entry commit: %w", err)
	}

	return entry, nil
}

// GetByID retrieves an entry by its ID with movie and ratings
func (r *EntryRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	query := `
		SELECT e.id, e.movie_id, e.group_number, e.position, e.added_at, e.picked_by_person_id,
		       m.id, m.created_at, m.updated_at, m.title, m.release_year, m.poster_url, m.synopsis, m.runtime_minutes, m.tmdb_id, m.imdb_id, m.metadata_json,
		       p.id, p.initial, p.name
		FROM entries e
		JOIN movies m ON e.movie_id = m.id
		LEFT JOIN persons p ON e.picked_by_person_id = p.id
		WHERE e.id = $1`

	entry := &model.Entry{}
	movie := &model.Movie{}
	var pickedByPersonDBID *uuid.UUID
	var pickedByInitial *string
	var pickedByName *string

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&entry.ID,
		&entry.MovieID,
		&entry.GroupNumber,
		&entry.Position,
		&entry.AddedAt,
		&entry.PickedByPersonID,
		&movie.ID,
		&movie.CreatedAt,
		&movie.UpdatedAt,
		&movie.Title,
		&movie.ReleaseYear,
		&movie.PosterURL,
		&movie.Synopsis,
		&movie.RuntimeMinutes,
		&movie.TMDBId,
		&movie.IMDBId,
		&movie.MetadataJSON,
		&pickedByPersonDBID,
		&pickedByInitial,
		&pickedByName,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entry by id: %w", err)
	}

	entry.Movie = movie
	applyPickedByPerson(entry, pickedByPersonDBID, pickedByInitial, pickedByName)

	// Fetch ratings with person info
	ratings, err := r.getRatingsForEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	entry.Ratings = ratings

	return entry, nil
}

// GetByMovieAndGroup retrieves an entry by movie ID and group number
func (r *EntryRepository) GetByMovieAndGroup(ctx context.Context, movieID uuid.UUID, groupNumber int) (*model.Entry, error) {
	query := `
		SELECT id, movie_id, group_number, position, added_at, picked_by_person_id
		FROM entries
		WHERE movie_id = $1 AND group_number = $2`

	entry := &model.Entry{}
	err := r.pool.QueryRow(ctx, query, movieID, groupNumber).Scan(
		&entry.ID,
		&entry.MovieID,
		&entry.GroupNumber,
		&entry.Position,
		&entry.AddedAt,
		&entry.PickedByPersonID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entry by movie and group: %w", err)
	}

	return entry, nil
}

// getRatingsForEntry fetches all ratings for an entry with person information
func (r *EntryRepository) getRatingsForEntry(ctx context.Context, entryID uuid.UUID) ([]*model.Rating, error) {
	query := `
		SELECT r.id, r.person_id, r.entry_id, r.score, r.created_at, r.updated_at,
		       p.id, p.initial, p.name
		FROM ratings r
		JOIN persons p ON r.person_id = p.id
		WHERE r.entry_id = $1
		ORDER BY p.initial`

	rows, err := r.pool.Query(ctx, query, entryID)
	if err != nil {
		return nil, fmt.Errorf("get ratings for entry: %w", err)
	}
	defer rows.Close()

	var ratings []*model.Rating
	for rows.Next() {
		rating := &model.Rating{}
		person := &model.Person{}
		if err := rows.Scan(
			&rating.ID,
			&rating.PersonID,
			&rating.EntryID,
			&rating.Score,
			&rating.CreatedAt,
			&rating.UpdatedAt,
			&person.ID,
			&person.Initial,
			&person.Name,
		); err != nil {
			return nil, fmt.Errorf("scan rating: %w", err)
		}
		rating.Person = person
		ratings = append(ratings, rating)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ratings rows: %w", err)
	}

	return ratings, nil
}

// getRatingsForEntries fetches all ratings for multiple entries with person information
func (r *EntryRepository) getRatingsForEntries(ctx context.Context, entryIDs []uuid.UUID) (map[uuid.UUID][]*model.Rating, error) {
	ratingsByEntry := make(map[uuid.UUID][]*model.Rating, len(entryIDs))
	if len(entryIDs) == 0 {
		return ratingsByEntry, nil
	}

	query := `
		SELECT r.id, r.person_id, r.entry_id, r.score, r.created_at, r.updated_at,
		       p.id, p.initial, p.name
		FROM ratings r
		JOIN persons p ON r.person_id = p.id
		WHERE r.entry_id = ANY($1)
		ORDER BY r.entry_id, p.initial`

	rows, err := r.pool.Query(ctx, query, entryIDs)
	if err != nil {
		return nil, fmt.Errorf("get ratings for entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		rating := &model.Rating{}
		person := &model.Person{}
		if err := rows.Scan(
			&rating.ID,
			&rating.PersonID,
			&rating.EntryID,
			&rating.Score,
			&rating.CreatedAt,
			&rating.UpdatedAt,
			&person.ID,
			&person.Initial,
			&person.Name,
		); err != nil {
			return nil, fmt.Errorf("scan rating: %w", err)
		}
		rating.Person = person
		ratingsByEntry[rating.EntryID] = append(ratingsByEntry[rating.EntryID], rating)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ratings rows: %w", err)
	}

	return ratingsByEntry, nil
}

// ListByGroup retrieves all entries for a specific group with movie and ratings
func (r *EntryRepository) ListByGroup(ctx context.Context, groupNumber int) ([]*model.Entry, error) {
	query := `
		SELECT e.id, e.movie_id, e.group_number, e.position, e.added_at, e.picked_by_person_id,
		       m.id, m.created_at, m.updated_at, m.title, m.release_year, m.poster_url, m.synopsis, m.runtime_minutes, m.tmdb_id, m.imdb_id, m.metadata_json,
		       p.id, p.initial, p.name
		FROM entries e
		JOIN movies m ON e.movie_id = m.id
		LEFT JOIN persons p ON e.picked_by_person_id = p.id
		WHERE e.group_number = $1
		ORDER BY e.position DESC`

	rows, err := r.pool.Query(ctx, query, groupNumber)
	if err != nil {
		return nil, fmt.Errorf("list entries by group: %w", err)
	}
	defer rows.Close()

	var entries []*model.Entry
	for rows.Next() {
		entry := &model.Entry{}
		movie := &model.Movie{}
		var pickedByPersonDBID *uuid.UUID
		var pickedByInitial *string
		var pickedByName *string

		if err := rows.Scan(
			&entry.ID,
			&entry.MovieID,
			&entry.GroupNumber,
			&entry.Position,
			&entry.AddedAt,
			&entry.PickedByPersonID,

			&movie.ID,
			&movie.CreatedAt,
			&movie.UpdatedAt,
			&movie.Title,
			&movie.ReleaseYear,
			&movie.PosterURL,
			&movie.Synopsis,
			&movie.RuntimeMinutes,
			&movie.TMDBId,
			&movie.IMDBId,
			&movie.MetadataJSON,
			&pickedByPersonDBID,
			&pickedByInitial,
			&pickedByName,
		); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entry.Movie = movie
		applyPickedByPerson(entry, pickedByPersonDBID, pickedByInitial, pickedByName)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list entries by group rows: %w", err)
	}

	entryIDs := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		entryIDs = append(entryIDs, entry.ID)
	}

	ratingsByEntry, err := r.getRatingsForEntries(ctx, entryIDs)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		entry.Ratings = ratingsByEntry[entry.ID]
	}

	return entries, nil
}

// ListGroups returns all unique group numbers in ascending order
func (r *EntryRepository) ListGroups(ctx context.Context) ([]int, error) {
	query := `SELECT DISTINCT group_number FROM entries ORDER BY group_number`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []int
	for rows.Next() {
		var group int
		if err := rows.Scan(&group); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups rows: %w", err)
	}

	return groups, nil
}

// GetCurrentGroup returns the highest group number, or 1 if no entries exist
func (r *EntryRepository) GetCurrentGroup(ctx context.Context) (int, error) {
	query := `SELECT COALESCE(MAX(group_number), 1) FROM entries`

	var group int
	err := r.pool.QueryRow(ctx, query).Scan(&group)
	if err != nil {
		return 1, fmt.Errorf("get current group: %w", err)
	}

	return group, nil
}

var (
	// ErrEntryNotFound is returned when the entry to update does not exist.
	ErrEntryNotFound = errors.New("entry not found")
	// ErrEntryExistsInGroup is returned when moving an entry into a group
	// that already contains the same movie.
	ErrEntryExistsInGroup = errors.New("movie already exists in target group")
	// ErrEntryGroupChanged is returned when concurrent moves kept changing the
	// entry's group and the move gave up after maxMoveAttempts.
	ErrEntryGroupChanged = errors.New("entry group changed concurrently")
)

// maxMoveAttempts bounds retries when a concurrent move changes the entry's
// group between reading it and acquiring the group locks.
const maxMoveAttempts = 3

// Update updates an existing entry. Changing the group moves the entry to the
// end of the target group under the same per-group advisory locks used by
// Create and ReorderEntries.
func (r *EntryRepository) Update(ctx context.Context, id uuid.UUID, input model.UpdateEntryInput) error {
	var err error
	for attempt := 0; attempt < maxMoveAttempts; attempt++ {
		err = r.update(ctx, id, input)
		if !errors.Is(err, ErrEntryGroupChanged) {
			return err
		}
	}
	return err
}

func (r *EntryRepository) update(ctx context.Context, id uuid.UUID, input model.UpdateEntryInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("update entry begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if input.GroupNumber != nil {
		if err := moveEntryToGroup(ctx, tx, id, *input.GroupNumber); err != nil {
			return err
		}
	}

	query := `
		UPDATE entries
		SET picked_by_person_id = CASE
		    	WHEN $2::uuid IS NULL THEN picked_by_person_id
		    	WHEN $2::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		    	ELSE $2::uuid
		    END
		WHERE id = $1`

	if _, err := tx.Exec(ctx, query, id, input.PickedByPersonID); err != nil {
		return fmt.Errorf("update entry: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("update entry commit: %w", err)
	}
	return nil
}

// moveEntryToGroup moves an entry to the end of targetGroup within tx.
func moveEntryToGroup(ctx context.Context, tx pgx.Tx, id uuid.UUID, targetGroup int) error {
	var sourceGroup int
	var movieID uuid.UUID
	err := tx.QueryRow(ctx, "SELECT group_number, movie_id FROM entries WHERE id = $1", id).Scan(&sourceGroup, &movieID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("move entry read group: %w", err)
	}
	if sourceGroup == targetGroup {
		return nil
	}

	// Lock both groups in ascending order so concurrent moves cannot deadlock.
	for _, group := range []int{min(sourceGroup, targetGroup), max(sourceGroup, targetGroup)} {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, $1)", group); err != nil {
			return fmt.Errorf("move entry lock group: %w", err)
		}
	}

	// Group changes only happen while holding the source group's lock, so a
	// mismatch here means another move won the race; retry with fresh locks.
	var lockedGroup int
	err = tx.QueryRow(ctx, "SELECT group_number FROM entries WHERE id = $1 FOR UPDATE", id).Scan(&lockedGroup)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("move entry recheck group: %w", err)
	}
	if lockedGroup != sourceGroup {
		return ErrEntryGroupChanged
	}

	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM entries WHERE movie_id = $1 AND group_number = $2)",
		movieID, targetGroup,
	).Scan(&exists); err != nil {
		return fmt.Errorf("move entry check duplicate: %w", err)
	}
	if exists {
		return ErrEntryExistsInGroup
	}

	if _, err := tx.Exec(ctx, `
		UPDATE entries
		SET group_number = $2,
		    position = COALESCE((SELECT MAX(position) FROM entries WHERE group_number = $2), 0) + 1
		WHERE id = $1`,
		id, targetGroup,
	); err != nil {
		return fmt.Errorf("move entry: %w", err)
	}
	return nil
}

// Delete removes an entry from the database
func (r *EntryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM entries WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

// ReorderEntries updates the positions of entries within a group
// entryIDs should be in the desired visual order (first = highest position, displayed first)
func (r *EntryRepository) ReorderEntries(ctx context.Context, groupNumber int, entryIDs []uuid.UUID) error {
	if len(entryIDs) == 0 {
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("reorder entries begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Serialize reorders per group to avoid conflicting position updates.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, $1)", groupNumber); err != nil {
		return fmt.Errorf("reorder entries lock group: %w", err)
	}

	var groupCount int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM entries WHERE group_number = $1 AND id = ANY($2::uuid[])", groupNumber, entryIDs).Scan(&groupCount); err != nil {
		return fmt.Errorf("reorder entries count group: %w", err)
	}
	if groupCount != len(entryIDs) {
		return fmt.Errorf("reorder entries count mismatch: group has %d matching entries, request has %d", groupCount, len(entryIDs))
	}

	// Assign positions in reverse order: first visual item gets highest position
	// (since display is ORDER BY position DESC)
	positions := make([]int, len(entryIDs))
	for i := range entryIDs {
		positions[i] = len(entryIDs) - i
	}

	// Move current positions out of the way to avoid unique constraint conflicts.
	if _, err := tx.Exec(ctx, `
		UPDATE entries
		SET position = -position
		WHERE group_number = $1 AND id = ANY($2::uuid[])`,
		groupNumber,
		entryIDs,
	); err != nil {
		return fmt.Errorf("reorder entries temp positions: %w", err)
	}

	query := `
		UPDATE entries AS e
		SET position = v.position
		FROM (
			SELECT unnest($1::uuid[]) AS id, unnest($2::int[]) AS position
		) AS v
		WHERE e.id = v.id AND e.group_number = $3`
	_, err = tx.Exec(ctx, query, entryIDs, positions, groupNumber)
	if err != nil {
		return fmt.Errorf("update entry positions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reorder entries commit: %w", err)
	}

	return nil
}
