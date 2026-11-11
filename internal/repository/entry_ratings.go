package repository

import (
	"context"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// loadRatings sets each entry's ratings, with person information, ordered by
// the person's initial.
func (r *EntryRepository) loadRatings(ctx context.Context, entries []*model.Entry) error {
	return loadEntryRatings(ctx, r.pool, entries)
}

type entryQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadEntryRatings(ctx context.Context, db entryQuerier, entries []*model.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]*model.Entry, len(entries))
	entryIDs := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
		entryIDs = append(entryIDs, entry.ID)
	}

	query := `
		SELECT r.id, r.person_id, r.entry_id, r.score, r.created_at, r.updated_at,
		       p.id, p.initial, p.name, p.color
		FROM ratings r
		JOIN persons p ON r.person_id = p.id
		WHERE r.entry_id = ANY($1)
		ORDER BY r.entry_id, p.initial`

	rows, err := db.Query(ctx, query, entryIDs)
	if err != nil {
		return fmt.Errorf("get ratings for entries: %w", err)
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
			&person.Color,
		); err != nil {
			return fmt.Errorf("scan rating: %w", err)
		}
		rating.Person = person
		entry := byID[rating.EntryID]
		entry.Ratings = append(entry.Ratings, rating)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate ratings rows: %w", err)
	}
	return nil
}
