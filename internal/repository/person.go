package repository

import (
	"context"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PersonRepository handles database operations for persons (read-only)
type PersonRepository struct {
	pool *pgxpool.Pool
}

// NewPersonRepository creates a new PersonRepository
func NewPersonRepository(pool *pgxpool.Pool) *PersonRepository {
	return &PersonRepository{pool: pool}
}

// GetAll retrieves all persons ordered by initial
func (r *PersonRepository) GetAll(ctx context.Context) ([]*model.Person, error) {
	query := `SELECT id, initial, name FROM persons ORDER BY initial`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all persons: %w", err)
	}
	defer rows.Close()

	var persons []*model.Person
	for rows.Next() {
		person := &model.Person{}
		if err := rows.Scan(&person.ID, &person.Initial, &person.Name); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		persons = append(persons, person)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persons: %w", err)
	}

	return persons, nil
}
