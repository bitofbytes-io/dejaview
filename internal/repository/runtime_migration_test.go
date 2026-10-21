package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestNormalizeUnknownRuntimeMigrationPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx := context.Background()
	var picker uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM persons LIMIT 1").Scan(&picker); err != nil {
		t.Fatal(err)
	}
	zero, positive := 0, 125
	ids := make([]uuid.UUID, 0, 3)
	for _, runtime := range []*int{&zero, &positive, nil} {
		var movie uuid.UUID
		if err := pool.QueryRow(ctx, "INSERT INTO movies(title,runtime_minutes) VALUES('Runtime fixture',$1) RETURNING id", runtime).Scan(&movie); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, movie)
		if _, err := NewEntryRepository(pool).Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 1, PickedByPersonID: &picker}); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile("../../migrations/014_normalize_unknown_runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(content), "-- +goose Down")
	// Reapplying the forward migration is safe; rolling back must not invent
	// zeros for unknown values that were already NULL before the migration.
	for _, sql := range []string{parts[0], parts[0], parts[1]} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
		for i, id := range ids {
			var runtime *int
			if err := pool.QueryRow(ctx, "SELECT runtime_minutes FROM movies WHERE id=$1", id).Scan(&runtime); err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				if runtime == nil || *runtime != positive {
					t.Fatalf("positive runtime changed: %v", runtime)
				}
			} else if runtime != nil {
				t.Fatalf("unknown runtime is not NULL: %d", *runtime)
			}
		}
	}
	stats, err := NewStatsRepository(pool).GetTrophyStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, stat := range stats {
		if stat.Person.ID == picker && (stat.PicksWithKnownRuntime != 1 || stat.TotalRuntimePicked != 125) {
			t.Fatalf("marathon runtime=%d picks=%d, want 125/1", stat.TotalRuntimePicked, stat.PicksWithKnownRuntime)
		}
	}
}
