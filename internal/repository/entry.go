package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// entrySelect reads an entry with its movie and picker. Callers append the
// WHERE and ORDER BY clauses and read rows with scanEntry.
const entrySelect = `
	SELECT e.id, e.movie_id, e.group_number, e.position, e.added_at, e.picked_by_person_id,
	       m.id, m.created_at, m.updated_at, m.title, m.release_year, m.poster_url, m.synopsis, m.runtime_minutes, m.tmdb_id, m.imdb_id, m.metadata_json,
	       p.id, p.initial, p.name, p.color
	FROM entries e
	JOIN movies m ON e.movie_id = m.id
	LEFT JOIN persons p ON e.picked_by_person_id = p.id`

// scanEntry reads one entrySelect row.
func scanEntry(row pgx.Row) (*model.Entry, error) {
	entry := &model.Entry{}
	movie := &model.Movie{}
	var pickedByPersonDBID *uuid.UUID
	var pickedByInitial *string
	var pickedByName *string
	var pickedByColor *string

	if err := row.Scan(
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
		&pickedByColor,
	); err != nil {
		return nil, err
	}

	entry.Movie = movie
	if pickedByPersonDBID != nil && pickedByInitial != nil && pickedByName != nil && pickedByColor != nil {
		entry.PickedByPerson = &model.Person{
			ID:      *pickedByPersonDBID,
			Initial: *pickedByInitial,
			Name:    *pickedByName,
			Color:   *pickedByColor,
		}
	}
	return entry, nil
}

// Create adds a movie to the end of a group. The group must be an existing
// group or the next new one (1 through the highest group + 1); otherwise
// Create returns ErrInvalidGroup. It returns ErrEntryExistsInGroup when the
// movie is already in the group. An uncertain commit returns an error wrapping
// ErrCommitOutcomeUnknown and the original cause.
func (r *EntryRepository) Create(ctx context.Context, input model.CreateEntryInput) (*model.Entry, error) {
	version, err := r.groupVersion(ctx, input.GroupVersion)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("create entry begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockEntryGroups(ctx, tx); err != nil {
		return nil, err
	}

	if err := checkGroupVersion(ctx, tx, version); err != nil {
		return nil, err
	}

	// Serialize position assignment per group to avoid duplicate positions under concurrency.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, $1)", input.GroupNumber); err != nil {
		return nil, fmt.Errorf("create entry lock group: %w", err)
	}

	if err := checkGroup(ctx, tx, input.GroupNumber); err != nil {
		return nil, err
	}

	// Insert with position = max position in group + 1 (or 1 if no entries in group)
	query := `
		INSERT INTO entries (movie_id, group_number, picked_by_person_id, position)
		VALUES ($1, $2, $3, COALESCE((SELECT MAX(position) FROM entries WHERE group_number = $2), 0) + 1)
		ON CONFLICT (movie_id, group_number) DO NOTHING
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
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEntryExistsInGroup
	}
	if err != nil {
		return nil, fmt.Errorf("create entry: %w", err)
	}

	if err := commitWrite(ctx, tx); err != nil {
		return nil, fmt.Errorf("create entry commit: %w", err)
	}

	return entry, nil
}

// GetByID retrieves an entry by its ID with movie and ratings
func (r *EntryRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	entry, err := scanEntry(r.pool.QueryRow(ctx, entrySelect+` WHERE e.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entry by id: %w", err)
	}

	if err := r.loadRatings(ctx, []*model.Entry{entry}); err != nil {
		return nil, err
	}
	return entry, nil
}

// ListAll retrieves every entry with movie and ratings, newest group first
// and, within a group, in display order (highest position first).
func (r *EntryRepository) ListAll(ctx context.Context) ([]*model.Entry, error) {
	return r.listAll(ctx, r.pool)
}

// ListDashboard reads group state and displayed entries from the same snapshot.
func (r *EntryRepository) ListDashboard(ctx context.Context) ([]*model.Entry, model.EntryGroupState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, model.EntryGroupState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := readGroupState(ctx, tx)
	if err != nil {
		return nil, state, err
	}
	entries, err := r.listAll(ctx, tx)
	return entries, state, err
}

func (r *EntryRepository) listAll(ctx context.Context, db entryQuerier) ([]*model.Entry, error) {
	rows, err := db.Query(ctx, entrySelect+`
		ORDER BY e.group_number DESC, e.position DESC`)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	var entries []*model.Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list entries rows: %w", err)
	}

	if err := loadEntryRatings(ctx, db, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

var (
	// ErrGroupNumberingChanged rejects a numeric target invalidated by deletion
	// or compaction. Refresh the selection before retrying.
	ErrGroupNumberingChanged = errors.New("group numbering changed; refresh and try again")
	// ErrEntryNotFound is returned when the entry to update, delete, or rate
	// does not exist.
	ErrEntryNotFound = errors.New("entry not found")
	// ErrPersonNotFound is returned when a picker or rating names a person
	// that does not exist.
	ErrPersonNotFound = errors.New("person not found")
	// ErrEntryExistsInGroup is returned when adding or moving an entry into a
	// group that already contains the same movie.
	ErrEntryExistsInGroup = errors.New("movie already exists in target group")
	// ErrReorderMismatch is returned when a reorder does not list exactly
	// the group's current entries, for example after another change.
	ErrReorderMismatch = errors.New("reorder does not match the group's entries")
	// ErrInvalidGroup is returned when adding or moving to a group that is
	// neither an existing group nor the next new one.
	ErrInvalidGroup = errors.New("group must be an existing group or the next new one")
	// ErrEntryGroupChanged is returned when concurrent moves kept changing the
	// entry's group and the move gave up after maxMoveAttempts.
	ErrEntryGroupChanged = errors.New("entry group changed concurrently")
)

// isForeignKeyViolation reports whether err is a Postgres foreign key
// violation of the named constraint.
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

// Update updates an existing entry. Changing the group moves the entry to the
// end of the target group under the global and per-group advisory locks
// used by Create and ReorderEntries. Empty groups are compacted before commit.
// It returns ErrEntryNotFound for a missing entry,
// ErrInvalidGroup for a target group that is neither an existing group nor
// the next new one, and ErrPersonNotFound for an unknown picker.
func (r *EntryRepository) Update(ctx context.Context, id uuid.UUID, input model.UpdateEntryInput) error {
	if input.GroupNumber != nil {
		version, err := r.groupVersion(ctx, input.GroupVersion)
		if err != nil {
			return err
		}
		input.GroupVersion = &version
	}
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

	if err := lockEntryGroups(ctx, tx); err != nil {
		return err
	}

	if input.GroupNumber != nil {
		if err := checkGroupVersion(ctx, tx, *input.GroupVersion); err != nil {
			return err
		}
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

	tag, err := tx.Exec(ctx, query, id, input.PickedByPersonID)
	if isForeignKeyViolation(err, "entries_picked_by_person_id_fkey") {
		return ErrPersonNotFound
	}
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEntryNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("update entry commit: %w", err)
	}
	return nil
}

// Delete removes an entry from the database. It returns ErrEntryNotFound when
// the entry does not exist.
func (r *EntryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("delete entry begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockEntryGroups(ctx, tx); err != nil {
		return err
	}
	var sourceGroup int
	err = tx.QueryRow(ctx, "DELETE FROM entries WHERE id = $1 RETURNING group_number", id).Scan(&sourceGroup)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if err := compactEntryGroups(ctx, tx, sourceGroup); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete entry commit: %w", err)
	}
	return nil
}
