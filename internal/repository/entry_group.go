package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CheckGroup returns ErrInvalidGroup unless group is an existing group or
// the next new one. Create checks again under the global group lock; this lets a
// caller reject a stale group before writing anything else.
func (r *EntryRepository) CheckGroup(ctx context.Context, group int) error {
	return checkGroup(ctx, r.pool, group)
}

func checkGroup(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, group int) error {
	var maxGroup int
	if err := db.QueryRow(ctx, "SELECT COALESCE(MAX(group_number), 0) FROM entries").Scan(&maxGroup); err != nil {
		return fmt.Errorf("read groups: %w", err)
	}
	if group < 1 || group > maxGroup+1 {
		return ErrInvalidGroup
	}
	return nil
}

// GroupState reads the current numeric-selection generation without taking the
// write lock, so callers can capture it before waiting on locks or remote APIs.
func (r *EntryRepository) GroupState(ctx context.Context) (model.EntryGroupState, error) {
	return readGroupState(ctx, r.pool)
}

func readGroupState(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (model.EntryGroupState, error) {
	var state model.EntryGroupState
	err := db.QueryRow(ctx, "SELECT version, first_group_capacity FROM entry_group_state WHERE singleton").Scan(&state.Version, &state.FirstGroupCapacity)
	if err != nil {
		return state, fmt.Errorf("read entry group state: %w", err)
	}
	return state, nil
}

func (r *EntryRepository) groupVersion(ctx context.Context, supplied *int64) (int64, error) {
	if supplied != nil {
		return *supplied, nil
	}
	state, err := r.GroupState(ctx)
	return state.Version, err
}

func checkGroupVersion(ctx context.Context, tx pgx.Tx, expected int64) error {
	state, err := readGroupState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Version != expected {
		return ErrGroupNumberingChanged
	}
	return nil
}

// maxMoveAttempts bounds retries when a concurrent move changes the entry's
// group between reading it and acquiring the group locks.
const maxMoveAttempts = 3

// moveEntryToGroup moves an entry to the end of targetGroup within tx. It
// returns with the entry row locked and empty groups compacted. The target
// group number shifts down if removing the source left a gap before it.
func moveEntryToGroup(ctx context.Context, tx pgx.Tx, id uuid.UUID, targetGroup int) error {
	if err := lockEntryGroups(ctx, tx); err != nil {
		return err
	}
	var sourceGroup int
	var movieID uuid.UUID
	err := tx.QueryRow(ctx, "SELECT group_number, movie_id FROM entries WHERE id = $1", id).Scan(&sourceGroup, &movieID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("move entry read group: %w", err)
	}

	// Lock both groups in ascending order so concurrent moves cannot deadlock.
	// A same-group request still takes the lock so the recheck below cannot
	// miss a concurrent move that has not committed yet.
	groups := []int{min(sourceGroup, targetGroup)}
	if sourceGroup != targetGroup {
		groups = append(groups, max(sourceGroup, targetGroup))
	}
	for _, group := range groups {
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
	if sourceGroup == targetGroup {
		return nil
	}

	// Like Create, the target must be an existing group or the next new one.
	// The moved entry does not count, so moving the only entry of the last
	// group cannot skip a group number.
	var maxGroup int
	if err := tx.QueryRow(ctx,
		"SELECT COALESCE(MAX(group_number), 0) FROM entries WHERE id <> $1", id,
	).Scan(&maxGroup); err != nil {
		return fmt.Errorf("move entry read groups: %w", err)
	}
	if targetGroup < 1 || targetGroup > maxGroup+1 {
		return ErrInvalidGroup
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
	return compactEntryGroups(ctx, tx, sourceGroup)
}

// lockEntryGroups reserves (1, 0) for all entry mutations, before any group
// reads, per-group locks, or row locks. Group numbers start at 1. This keeps
// inserts and reorders from racing with renumbering in another transaction.
func lockEntryGroups(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, 0)"); err != nil {
		return fmt.Errorf("lock entry groups: %w", err)
	}
	return nil
}

// compactEntryGroups requires lockEntryGroups. Move groups in ascending order
// into empty numbers: a single dense-rank UPDATE can violate the immediate
// movie/group and group/position unique constraints before other rows move.
// Only group numbers change; entry IDs, positions, ratings, and pickers survive.
func compactEntryGroups(ctx context.Context, tx pgx.Tx, sourceGroup int) error {
	var remains bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM entries WHERE group_number=$1)", sourceGroup).Scan(&remains); err != nil {
		return fmt.Errorf("check emptied entry group: %w", err)
	}
	if remains {
		return nil
	}
	// Advance even when the last group disappears: its old number can be
	// reused as the next new group, but it no longer identifies the same group.
	if _, err := tx.Exec(ctx, `UPDATE entry_group_state SET version=version+1,
		first_group_capacity=CASE WHEN $1=1 THEN 9 ELSE first_group_capacity END WHERE singleton`, sourceGroup); err != nil {
		return fmt.Errorf("advance entry group state: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT DISTINCT group_number FROM entries ORDER BY group_number")
	if err != nil {
		return fmt.Errorf("read groups to compact: %w", err)
	}
	groups, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return fmt.Errorf("scan groups to compact: %w", err)
	}
	for i, group := range groups {
		if group == i+1 {
			continue
		}
		if _, err := tx.Exec(ctx, "UPDATE entries SET group_number = $1 WHERE group_number = $2", i+1, group); err != nil {
			return fmt.Errorf("compact entry group: %w", err)
		}
	}
	return nil
}
