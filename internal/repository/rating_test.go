package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Each run uses its own schema and applies the actual forward migrations.
func ratingTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DEJAVIEW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEJAVIEW_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "rating_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	files, err := filepath.Glob("../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.Split(string(content), "-- +goose Down")[0]
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	return pool
}

type storedRating struct {
	PersonID uuid.UUID
	Score    float64
}

// entryRatings reads an entry's stored ratings straight from the table.
func entryRatings(ctx context.Context, pool *pgxpool.Pool, entryID uuid.UUID) ([]storedRating, error) {
	rows, err := pool.Query(ctx, `SELECT person_id, score FROM ratings WHERE entry_id = $1`, entryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[storedRating])
}

func TestSaveBatchPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewRatingRepository(pool)
	var movieID, entryID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ('Review fixture') RETURNING id`).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO entries(movie_id, group_number, position) VALUES ($1,1,1) RETURNING id`, movieID).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id FROM persons ORDER BY initial`)
	if err != nil {
		t.Fatal(err)
	}
	people, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	score := func(value float64) *float64 { return &value }
	if err := repo.SaveBatch(ctx, entryID, []model.RatingChange{{PersonID: people[0], Score: score(3)}, {PersonID: people[1], Score: score(4)}, {PersonID: people[2], Score: score(5)}}); err != nil {
		t.Fatal(err)
	}
	// The missing person fails after an update and a delete; both must roll back.
	err = repo.SaveBatch(ctx, entryID, []model.RatingChange{{PersonID: people[0], Score: score(9)}, {PersonID: people[1]}, {PersonID: uuid.New(), Score: score(7)}})
	if err == nil {
		t.Fatal("expected foreign-key failure")
	}
	ratings, err := entryRatings(ctx, pool, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ratings) != 3 {
		t.Fatalf("rollback retained %d ratings, want 3", len(ratings))
	}
	for _, rating := range ratings {
		if rating.PersonID == people[0] && rating.Score != 3 {
			t.Fatalf("update survived rollback: %v", rating.Score)
		}
	}
	if err := repo.SaveBatch(ctx, entryID, []model.RatingChange{{PersonID: people[0], Score: score(0)}, {PersonID: people[1]}, {PersonID: people[3], Score: score(10)}}); err != nil {
		t.Fatal(err)
	}
	ratings, err = entryRatings(ctx, pool, entryID)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[uuid.UUID]float64{people[0]: 0, people[2]: 5, people[3]: 10}
	if len(ratings) != len(expected) {
		t.Fatalf("got %d ratings", len(ratings))
	}
	for _, rating := range ratings {
		want, ok := expected[rating.PersonID]
		if !ok || rating.Score != want {
			t.Fatalf("unexpected rating: %+v", rating)
		}
	}
}
