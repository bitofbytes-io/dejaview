package repository

import (
	"context"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RatingRepository handles database operations for ratings
type RatingRepository struct {
	pool *pgxpool.Pool
}

// NewRatingRepository creates a new RatingRepository
func NewRatingRepository(pool *pgxpool.Pool) *RatingRepository {
	return &RatingRepository{pool: pool}
}

// SaveBatch applies a validated submission atomically. Omitted people are unchanged.
func (r *RatingRepository) SaveBatch(ctx context.Context, entryID uuid.UUID, changes []model.RatingChange) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rating batch: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, change := range changes {
		if change.Score == nil {
			_, err = tx.Exec(ctx, `DELETE FROM ratings WHERE person_id = $1 AND entry_id = $2`, change.PersonID, entryID)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO ratings (person_id, entry_id, score) VALUES ($1, $2, $3)
    ON CONFLICT (person_id, entry_id) DO UPDATE SET score = EXCLUDED.score, updated_at = NOW()`, change.PersonID, entryID, *change.Score)
		}
		if err != nil {
			return fmt.Errorf("save rating batch: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rating batch: %w", err)
	}
	return nil
}
