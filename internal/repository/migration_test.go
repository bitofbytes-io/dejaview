package repository

import (
	"context"
	"fmt"
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

// TestFamilySeedRemovedOnlyFromNewInstalls covers migration 013, which drops
// migration 003's seeded people only while goose is creating the database.
func TestFamilySeedRemovedOnlyFromNewInstalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// install applies migrations 001-012 and records them as goose does, with
	// 001-003 applied created ago and 004-012 applied migrated ago.
	install := func(t *testing.T, created, migrated time.Duration) *pgxpool.Pool {
		t.Helper()
		pool := testSchemaPool(t)
		applyMigrations(t, pool, 1, 12)
		if _, err := pool.Exec(ctx, `CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp DEFAULT now())`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO goose_db_version (version_id, is_applied, tstamp)
			SELECT v, true, LOCALTIMESTAMP - CASE WHEN v <= 3 THEN $1::interval ELSE $2::interval END
			FROM generate_series(0, 12) v`,
			fmt.Sprintf("%d seconds", int(created.Seconds())), fmt.Sprintf("%d seconds", int(migrated.Seconds()))); err != nil {
			t.Fatal(err)
		}
		return pool
	}

	t.Run("new install", func(t *testing.T) {
		db := install(t, 0, 0)
		applyMigrations(t, db, 13, 13)
		if n := countPersons(t, ctx, db); n != 0 {
			t.Fatalf("new install kept %d seeded people", n)
		}
	})

	t.Run("original install", func(t *testing.T) {
		db := install(t, 300*24*time.Hour, 7*24*time.Hour)
		applyMigrations(t, db, 13, 13)
		if n := countPersons(t, ctx, db); n != 4 {
			t.Fatalf("original install has %d people, want 4", n)
		}
	})

	t.Run("install created by an earlier goose run", func(t *testing.T) {
		db := install(t, 2*time.Hour, 2*time.Hour)
		applyMigrations(t, db, 13, 13)
		if n := countPersons(t, ctx, db); n != 4 {
			t.Fatalf("earlier install has %d people, want 4", n)
		}
	})

	t.Run("new install whose seed was used", func(t *testing.T) {
		db := install(t, 0, 0)
		var entryID string
		if err := db.QueryRow(ctx, `WITH movie AS (INSERT INTO movies (title) VALUES ('Alien') RETURNING id)
			INSERT INTO entries (movie_id, group_number, position) SELECT id, 1, 1 FROM movie RETURNING id`).Scan(&entryID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO ratings (person_id, entry_id, score) SELECT id, $1, 8 FROM persons WHERE initial = 'D'`, entryID); err != nil {
			t.Fatal(err)
		}
		applyMigrations(t, db, 13, 13)
		if n := countPersons(t, ctx, db); n != 4 {
			t.Fatalf("used seed has %d people, want 4", n)
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
