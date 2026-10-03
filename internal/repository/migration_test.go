package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func countPersons(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM persons`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestFamilySeedRemovedOnlyWhereUnused covers migration 013, which drops
// migration 003's seeded people from goose-managed databases with no ratings
// or picks.
func TestFamilySeedRemovedOnlyWhereUnused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// install applies migrations 001-012 and creates goose's version table.
	install := func(t *testing.T) *pgxpool.Pool {
		t.Helper()
		pool := testSchemaPool(t)
		applyMigrations(t, pool, 1, 12)
		if _, err := pool.Exec(ctx, `CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp DEFAULT now())`); err != nil {
			t.Fatal(err)
		}
		return pool
	}
	addEntry := func(t *testing.T, pool *pgxpool.Pool, pickerInitial string) string {
		t.Helper()
		var entryID string
		if err := pool.QueryRow(ctx, `WITH movie AS (INSERT INTO movies (title) VALUES ('Alien') RETURNING id)
			INSERT INTO entries (movie_id, group_number, position, picked_by_person_id)
			SELECT id, 1, 1, (SELECT id FROM persons WHERE initial = $1) FROM movie RETURNING id`, pickerInitial).Scan(&entryID); err != nil {
			t.Fatal(err)
		}
		return entryID
	}

	t.Run("new install", func(t *testing.T) {
		pool := install(t)
		applyMigrations(t, pool, 13, 13)
		if n := countPersons(t, ctx, pool); n != 0 {
			t.Fatalf("new install kept %d seeded people", n)
		}
	})

	t.Run("entries without picks or ratings", func(t *testing.T) {
		pool := install(t)
		addEntry(t, pool, "")
		applyMigrations(t, pool, 13, 13)
		if n := countPersons(t, ctx, pool); n != 0 {
			t.Fatalf("unused seed kept %d people", n)
		}
	})

	t.Run("install with ratings", func(t *testing.T) {
		pool := install(t)
		entryID := addEntry(t, pool, "")
		if _, err := pool.Exec(ctx, `INSERT INTO ratings (person_id, entry_id, score) SELECT id, $1, 8 FROM persons WHERE initial = 'D'`, entryID); err != nil {
			t.Fatal(err)
		}
		applyMigrations(t, pool, 13, 13)
		if n := countPersons(t, ctx, pool); n != 4 {
			t.Fatalf("install with ratings has %d people, want 4", n)
		}
	})

	t.Run("install with picks", func(t *testing.T) {
		pool := install(t)
		addEntry(t, pool, "J")
		applyMigrations(t, pool, 13, 13)
		if n := countPersons(t, ctx, pool); n != 4 {
			t.Fatalf("install with picks has %d people, want 4", n)
		}
	})

	t.Run("without goose_db_version", func(t *testing.T) {
		pool := testSchemaPool(t)
		applyMigrations(t, pool, 1, 13)
		if n := countPersons(t, ctx, pool); n != 4 {
			t.Fatalf("SQL applied without goose has %d people, want 4", n)
		}
	})
}
