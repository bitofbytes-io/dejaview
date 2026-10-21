package main

import (
	"context"
	"database/sql"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// Applies every migration to an empty schema, twice, the way the deploy hook
// runs dejaview-migrate.
func TestRunAppliesMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, schema, databaseURL := testSchema(ctx, t)

	for range 2 {
		if err := run(ctx, databaseURL); err != nil {
			t.Fatal(err)
		}
	}

	if version := gooseVersion(ctx, t, admin, schema); version < 13 {
		t.Fatalf("goose version = %d, want at least 13", version)
	}
	var hasColor bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'persons' AND column_name = 'color')`, schema).Scan(&hasColor); err != nil {
		t.Fatal(err)
	}
	if !hasColor {
		t.Fatal("persons.color is missing after migrations")
	}
}

// Upgrades a database left at migration 012 with existing people, the
// production case dejaview-migrate was added for.
func TestRunUpgradesExistingDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, schema, databaseURL := testSchema(ctx, t)

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 12); err != nil {
		t.Fatal(err)
	}
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "INSERT INTO "+identifier+".persons (initial, name) VALUES ('B', 'Bea')"); err != nil {
		t.Fatal(err)
	}
	before := personNames(ctx, t, admin, schema)
	if version := gooseVersion(ctx, t, admin, schema); version != 12 {
		t.Fatalf("goose version before run = %d, want 12", version)
	}

	if err := run(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}

	if version := gooseVersion(ctx, t, admin, schema); version < 13 {
		t.Fatalf("goose version = %d, want at least 13", version)
	}
	if after := personNames(ctx, t, admin, schema); !maps.Equal(before, after) {
		t.Fatalf("persons changed during upgrade: before %v, after %v", before, after)
	}
	rows, err := admin.Query(ctx, "SELECT initial, color FROM "+identifier+".persons")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var initial, color string
	if _, err := pgx.ForEachRow(rows, []any{&initial, &color}, func() error {
		got[initial] = color
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// B sorts second by initial, so it gets the second palette colour.
	want := map[string]string{"D": "cyan", "J": "rose", "C": "amber", "A": "violet", "B": "rose"}
	if !maps.Equal(got, want) {
		t.Fatalf("person colours = %v, want %v", got, want)
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	if err := run(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run error = %v, want DATABASE_URL error", err)
	}
}

// testSchema creates a throwaway schema and returns an admin connection, the
// schema name, and a database URL whose search_path points at it.
func testSchema(ctx context.Context, t *testing.T) (*pgx.Conn, string, string) {
	t.Helper()
	databaseURL := os.Getenv("DEJAVIEW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEJAVIEW_TEST_DATABASE_URL is not set")
	}
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	schema := "migrate_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return admin, schema, parsed.String()
}

func gooseVersion(ctx context.Context, t *testing.T, admin *pgx.Conn, schema string) int64 {
	t.Helper()
	var version int64
	if err := admin.QueryRow(ctx, "SELECT max(version_id) FROM "+pgx.Identifier{schema}.Sanitize()+".goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// personNames maps each person's id to their initial and name.
func personNames(ctx context.Context, t *testing.T, admin *pgx.Conn, schema string) map[string]string {
	t.Helper()
	rows, err := admin.Query(ctx, "SELECT id::text, initial || ':' || name FROM "+pgx.Identifier{schema}.Sanitize()+".persons")
	if err != nil {
		t.Fatal(err)
	}
	people := map[string]string{}
	var id, person string
	if _, err := pgx.ForEachRow(rows, []any{&id, &person}, func() error {
		people[id] = person
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return people
}

// Exercises every Down section only inside a disposable schema. A successful
// second Up must not conceal objects that the rollback failed to remove.
func TestMigrationsRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, schema, databaseURL := testSchema(ctx, t)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	sources := provider.ListSources()
	if len(sources) == 0 {
		t.Fatal("no embedded migrations")
	}
	latest := sources[len(sources)-1].Version

	assertVersion := func(want int64) {
		t.Helper()
		got, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("migration version=%d, want %d", got, want)
		}
	}
	assertSchema := func(up bool) {
		t.Helper()
		var tables []string
		if err := admin.QueryRow(ctx, `SELECT COALESCE(array_agg(tablename::text ORDER BY tablename), '{}'::text[])
   FROM pg_tables WHERE schemaname = $1 AND tablename <> 'goose_db_version'`, schema).Scan(&tables); err != nil {
			t.Fatal(err)
		}
		var want []string
		if up {
			want = []string{"entries", "entry_group_state", "movies", "persons", "ratings"}
		}
		if !slices.Equal(tables, want) {
			t.Fatalf("up=%t: application tables=%v, want %v", up, tables, want)
		}
		var hasRoutine bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc p
   JOIN pg_namespace n ON n.oid = p.pronamespace
   WHERE n.nspname = $1 AND p.proname = 'update_updated_at_column' AND p.pronargs = 0)`, schema).Scan(&hasRoutine); err != nil {
			t.Fatal(err)
		}
		if hasRoutine != up {
			t.Fatalf("up=%t: update_updated_at_column() exists=%t", up, hasRoutine)
		}
	}
	for pass := 1; pass <= 2; pass++ {
		if _, err := provider.Up(ctx); err != nil {
			t.Fatalf("Up pass %d: %v", pass, err)
		}
		assertVersion(latest)
		assertSchema(true)
		assertPickerDeleteSetsNull(ctx, t, admin, schema)
		if pass == 1 {
			if _, err := provider.DownTo(ctx, 0); err != nil {
				t.Fatalf("DownTo(0): %v", err)
			}
			assertVersion(0)
			assertSchema(false)
		}
	}
}

func assertPickerDeleteSetsNull(ctx context.Context, t *testing.T, admin *pgx.Conn, schema string) {
	t.Helper()
	identifier := pgx.Identifier{schema}.Sanitize()
	var pickerID, movieID, entryID uuid.UUID
	if err := admin.QueryRow(ctx, "INSERT INTO "+identifier+".persons (initial, name) VALUES ('X', 'Round-trip picker') RETURNING id").Scan(&pickerID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, "INSERT INTO "+identifier+".movies (title) VALUES ('Round-trip movie') RETURNING id").Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, "INSERT INTO "+identifier+".entries (movie_id, group_number, position, picked_by_person_id) VALUES ($1, 1, 1, $2) RETURNING id", movieID, pickerID).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	// No rating references this picker: deleting them must preserve their entry.
	result, err := admin.Exec(ctx, "DELETE FROM "+identifier+".persons WHERE id = $1", pickerID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("deleted %d pickers, want 1", result.RowsAffected())
	}
	var remainingMovie uuid.UUID
	var pickerIsNull bool
	if err := admin.QueryRow(ctx, "SELECT movie_id, picked_by_person_id IS NULL FROM "+identifier+".entries WHERE id = $1", entryID).Scan(&remainingMovie, &pickerIsNull); err != nil {
		t.Fatalf("entry did not survive picker deletion: %v", err)
	}
	if remainingMovie != movieID || !pickerIsNull {
		t.Fatalf("entry changed after picker deletion: movie=%s pickerIsNull=%t", remainingMovie, pickerIsNull)
	}
}
