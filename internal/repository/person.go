package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrPersonNotFound is returned when the person to change does not exist.
	ErrPersonNotFound = errors.New("person not found")
	// ErrPersonInitialTaken is returned when another person already has the initial.
	ErrPersonInitialTaken = errors.New("person initial already in use")
	// ErrPersonInUse is returned when a person with ratings or picks is deleted.
	ErrPersonInUse = errors.New("person has ratings or picks")
)

// PersonRepository handles database operations for persons
type PersonRepository struct {
	pool *pgxpool.Pool
}

// NewPersonRepository creates a new PersonRepository
func NewPersonRepository(pool *pgxpool.Pool) *PersonRepository {
	return &PersonRepository{pool: pool}
}

// GetAll retrieves all persons ordered by initial
func (r *PersonRepository) GetAll(ctx context.Context) ([]*model.Person, error) {
	query := `SELECT id, initial, name, color FROM persons ORDER BY initial`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all persons: %w", err)
	}
	defer rows.Close()

	var persons []*model.Person
	for rows.Next() {
		person := &model.Person{}
		if err := rows.Scan(&person.ID, &person.Initial, &person.Name, &person.Color); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		persons = append(persons, person)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persons: %w", err)
	}

	return persons, nil
}

// Create adds a person. It returns ErrPersonInitialTaken when another person
// already has the initial.
func (r *PersonRepository) Create(ctx context.Context, initial, name, color string) (*model.Person, error) {
	person := &model.Person{}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO persons (initial, name, color) VALUES ($1, $2, $3)
		RETURNING id, initial, name, color`,
		initial, name, color,
	).Scan(&person.ID, &person.Initial, &person.Name, &person.Color)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrPersonInitialTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create person: %w", err)
	}
	return person, nil
}

// Update renames a person and sets their colour. It returns ErrPersonNotFound
// when the person does not exist. The initial is fixed once created.
func (r *PersonRepository) Update(ctx context.Context, id uuid.UUID, name, color string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE persons SET name = $2, color = $3 WHERE id = $1`, id, name, color)
	if err != nil {
		return fmt.Errorf("update person: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// Delete removes a person who has no ratings and has not picked a movie, so
// it never erases scores or clears who picked an entry. It returns
// ErrPersonInUse for anyone else and ErrPersonNotFound for an unknown ID.
func (r *PersonRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM persons p
		WHERE p.id = $1
		  AND NOT EXISTS (SELECT 1 FROM ratings r WHERE r.person_id = p.id)
		  AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.picked_by_person_id = p.id)`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrPersonInUse // rated between the check and the delete
	}
	if err != nil {
		return fmt.Errorf("delete person: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM persons WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("delete person: %w", err)
	}
	if exists {
		return ErrPersonInUse
	}
	return ErrPersonNotFound
}
