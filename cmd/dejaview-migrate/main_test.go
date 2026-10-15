package main

import (
	"context"
	"database/sql"
	"maps"
	"net/url"
	"os"
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
	defer db.Close()
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
	t.Cleanup(func() { admin.Close(context.Background()) })
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
