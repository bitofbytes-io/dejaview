package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MovieRepository handles database operations for movies
type MovieRepository struct {
	pool *pgxpool.Pool
}

// NewMovieRepository creates a new MovieRepository
func NewMovieRepository(pool *pgxpool.Pool) *MovieRepository {
	return &MovieRepository{pool: pool}
}

// Create inserts a new movie into the database
func (r *MovieRepository) Create(ctx context.Context, input model.CreateMovieInput) (*model.Movie, error) {
	var metadataBytes []byte
	if input.MetadataJSON != nil {
		metadataBytes = input.MetadataJSON
	}

	query := `
		INSERT INTO movies (title, release_year, poster_url, synopsis, runtime_minutes, tmdb_id, imdb_id, metadata_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at, title, release_year, poster_url, synopsis, runtime_minutes, tmdb_id, imdb_id, metadata_json`

	movie := &model.Movie{}
	err := r.pool.QueryRow(ctx, query,
		input.Title,
		input.ReleaseYear,
		input.PosterURL,
		input.Synopsis,
		input.RuntimeMinutes,
		input.TMDBId,
		input.IMDBId,
		metadataBytes,
	).Scan(
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
	)
	if err != nil {
		return nil, fmt.Errorf("create movie: %w", err)
	}

	return movie, nil
}

// GetByTMDBId retrieves a movie by its TMDB ID
func (r *MovieRepository) GetByTMDBId(ctx context.Context, tmdbID int) (*model.Movie, error) {
	query := `
		SELECT id, created_at, updated_at, title, release_year, poster_url, synopsis, runtime_minutes, tmdb_id, imdb_id, metadata_json
		FROM movies
		WHERE tmdb_id = $1`

	movie := &model.Movie{}
	err := r.pool.QueryRow(ctx, query, tmdbID).Scan(
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get movie by tmdb id: %w", err)
	}

	return movie, nil
}
