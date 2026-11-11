package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReorderEntries updates the positions of entries within a group
// entryIDs should be in the desired visual order (first = highest position, displayed first)
// and list every entry in the group once; otherwise it returns ErrReorderMismatch.
func (r *EntryRepository) ReorderEntries(ctx context.Context, groupNumber int, entryIDs []uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("reorder entries begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockEntryGroups(ctx, tx); err != nil {
		return err
	}

	// Serialize reorders per group to avoid conflicting position updates.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1, $1)", groupNumber); err != nil {
		return fmt.Errorf("reorder entries lock group: %w", err)
	}

	// The request must list each of the group's entries exactly once.
	var matching, groupSize int
	if err := tx.QueryRow(ctx,
		"SELECT COUNT(*) FILTER (WHERE id = ANY($2::uuid[])), COUNT(*) FROM entries WHERE group_number = $1",
		groupNumber, entryIDs,
	).Scan(&matching, &groupSize); err != nil {
		return fmt.Errorf("reorder entries count group: %w", err)
	}
	if matching != len(entryIDs) || groupSize != len(entryIDs) {
		return fmt.Errorf("%w: group %d has %d entries, %d of them listed, request has %d",
			ErrReorderMismatch, groupNumber, groupSize, matching, len(entryIDs))
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
